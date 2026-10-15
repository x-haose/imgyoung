package channel

import (
	"context"
	"fmt"
	"maps"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
)

// completeTemplateConfig 补齐模板渠道随授权而来的后端参数(原地修改 config)。
// @Description: 新提交的授权(令牌不是哨兵)跑一遍后端的配置流程,后端写下的参数并入配置——
// onedrive 由此选定用户默认驱动器、写下 drive_id 与 drive_type。流程中途失败时已写下的参数照样并入:
// 其中可能有刷新后的令牌,刷新令牌只能用一次的服务商,丢掉它原令牌也已作废。沿用已存授权时不再跑流程,
// 已存配置里模板不产出的参数(授权带出与运行期写回的)随授权一并保留。自定义配置不经此步:
// 直填的 rclone 参数应自带这些值。
// @param ctx 控制后端查询的超时与取消
// @param templateID 模板 id;空或不需授权的模板不做任何事
// @param submitted 提交的配置,据其令牌是否为哨兵判断授权是否沿用
// @param config 已整理的配置
// @param stored 渠道已存的配置;创建场景为 nil
// @return 后端配置流程失败时的错误(后端报错、提问没有默认答案、步数超限)
func completeTemplateConfig(ctx context.Context, templateID string, submitted, config,
	stored map[string]string) error {
	tpl := findTemplate(templateID)
	if tpl == nil || !tpl.RequiresOAuth {
		return nil
	}
	if submitted[ConfigKeyToken] == SecretPlaceholder {
		owned := tpl.ownedKeys()
		for k, v := range stored {
			if !owned[k] {
				config[k] = v
			}
		}
		return nil
	}
	written, err := completeAuthorization(ctx, tpl.Driver, config)
	for k, v := range written {
		if v == "" {
			delete(config, k)
			continue
		}
		config[k] = v
	}
	return err
}

// ownedKeys 返回模板合并配置时会产出的键:固定预设、字段落位键与拼接的 endpoint。
// @return 键集合
func (t *Template) ownedKeys() map[string]bool {
	owned := make(map[string]bool, len(t.preset)+len(t.Fields)+1)
	for k := range t.preset {
		owned[k] = true
	}
	for _, f := range t.Fields {
		if f.configKey != "" {
			owned[f.configKey] = true
		}
	}
	if t.endpointPattern != "" {
		owned["endpoint"] = true
	}
	return owned
}

// completeAuthorization 以已取得的授权跑完后端的配置流程,返回后端在流程里写下的参数。
// @Description: 每一问都取默认答案,授权一步因令牌已在参数里而跳过,结果与 rclone config
// 逐题取默认的选择一致。流程里的查询用参数中的令牌;令牌若已过期而被刷新,新令牌也在写下的参数里。
// @param ctx 控制后端查询的超时与取消
// @param driver rclone 后端名
// @param params 含授权令牌的后端参数(可入库形态);不修改
// @return 后端写下的参数,值为空串表示清除该参数;流程失败时为失败前已写下的部分
// @return 后端不存在(ErrInvalidChannel)、后端报错、提问没有默认答案或超出步数上限时的错误
func completeAuthorization(ctx context.Context, driver string, params map[string]string) (map[string]string, error) {
	ri, err := findBackend(driver)
	if err != nil {
		return nil, err
	}
	written := configmap.Simple{}
	if ri.Config == nil {
		return written, nil
	}
	values := configmap.Simple{}
	maps.Copy(values, params)
	m := backendParams(ri, values)
	m.AddSetter(written)
	in := fs.ConfigIn{}
	for range configWalkLimit {
		out, err := ri.Config(ctx, oauthConfigName, m, in)
		if err != nil {
			return written, fmt.Errorf("后端 %q 授权后的配置失败: %w", ri.Name, err)
		}
		next, done, err := defaultStep(ri.Name, out)
		if err != nil {
			return written, err
		}
		if done {
			return written, nil
		}
		in = next
	}
	return written, fmt.Errorf("后端 %q 的配置流程超出 %d 步", ri.Name, configWalkLimit)
}

// defaultStep 解读配置状态机的一步输出,以默认答案前进:提问取默认值,授权一步跳过。
// @Description: 输出为空或下一状态为空即流程结束,与 rclone 自身驱动配置流程的判定一致。
// @param driver rclone 后端名,仅用于错误文案
// @param out 本步输出
// @return 下一步输入
// @return 流程是否已结束
// @return 后端报错或提问没有默认答案时的错误
func defaultStep(driver string, out *fs.ConfigOut) (fs.ConfigIn, bool, error) {
	switch {
	case out == nil:
		return fs.ConfigIn{}, true, nil
	case out.Error != "":
		return fs.ConfigIn{}, false, fmt.Errorf("后端 %q 配置失败: %s", driver, out.Error)
	case out.State == "":
		return fs.ConfigIn{}, true, nil
	case out.OAuth != nil:
		return fs.ConfigIn{State: out.State}, false, nil
	case out.Option != nil:
		if out.Option.Default == nil {
			return fs.ConfigIn{}, false, fmt.Errorf("后端 %q 要求填写 %s", driver, out.Option.Name)
		}
		return fs.ConfigIn{State: out.State, Result: fmt.Sprint(out.Option.Default)}, false, nil
	default:
		return fs.ConfigIn{State: out.State, Result: out.Result}, false, nil
	}
}
