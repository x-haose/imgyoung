// Package rclone 以 rclone 的 fs.Fs 实现 StorageDriver。
// 依赖 driver 包的接口,是可选(opt-in)后端:仅 import 本包才会拖入 rclone 依赖树。
package rclone

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/cache"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/object"
	"github.com/rclone/rclone/fs/walk"

	"github.com/x-haose/imgyoung/internal/store/driver"

	// 注册 rclone 全部后端。
	_ "github.com/rclone/rclone/backend/all"
)

// Options 构造驱动的全部参数。
type Options struct {
	Backend  string                  // rclone 后端名(local/s3/webdav 等)
	Params   map[string]string       // 后端参数,密码类值须已按 rclone 要求编码(见 ObscureSecrets);可为 nil
	Path     string                  // 后端内路径(桶名或目录);空为后端根
	Crypt    *CryptOptions           // 非 nil 时在后端之上套 crypt 加密层
	OnChange func(key, value string) // 后端运行期写回参数(刷新后的 OAuth 令牌、onedrive 的 drive_id 等)时的持久化回调;nil 表示写回只留在内存
}

// CryptOptions crypt 加密层参数;文件名加密固定取 standard。
type CryptOptions struct {
	Password string // 内容加密密码(明文,本包负责编码)
	Salt     string // 文件名加密盐(明文,即 rclone 的 password2)
}

// Driver 基于 rclone 后端 Fs 的 Driver 实现。
// @Description: 参数以映射直接交给后端,不经连接字符串、不经 rclone 配置文件。
// fs.Fs 本身并发安全,故本类型无内部状态锁。
type Driver struct {
	f         fs.Fs  // 对外使用的文件系统句柄;加密渠道为 crypt 层
	innerName string // 加密渠道内层 Fs 在 rclone fs 缓存中的登记名;非加密渠道为空
}

// 编译期断言:Driver 必须完整实现 Driver 接口。
var _ driver.Driver = (*Driver)(nil)

// Open 构造驱动。
// @Description: 参数映射挂本包的 setter,后端刷新 OAuth 令牌等写回时经 OnChange 持久化,
// 令牌不会只活在内存、重启即退回初始值。加密渠道的内层 Fs 以随机名登记进 rclone 的 fs
// 缓存,crypt 层按该名取用。错误链不携带参数——其中含凭证,任何回显(日志、API 错误消息)
// 都是泄露面。
// @param ctx 控制后端初始化的取消
// @param opts 构造参数
// @return 创建好的驱动实例,用毕须 Close
// @return 后端不存在或初始化失败时的错误
func Open(ctx context.Context, opts Options) (*Driver, error) {
	inner, err := newBackend(ctx, opts.Backend, opts.Path, withDefaults(opts.Backend, opts.Params), opts.OnChange)
	if err != nil {
		return nil, err
	}
	if opts.Crypt == nil {
		return &Driver{f: inner}, nil
	}
	innerName := inner.Name()
	cache.Put(innerName+":", inner)
	params, err := cryptParams(innerName, opts.Crypt)
	if err != nil {
		cache.ClearConfig(innerName)
		return nil, err
	}
	outer, err := newBackend(ctx, "crypt", "", params, nil)
	if err != nil {
		cache.ClearConfig(innerName)
		return nil, err
	}
	return &Driver{f: outer, innerName: innerName}, nil
}

// newBackend 以参数映射直接构造一个后端 Fs。
// @param ctx 控制初始化的取消
// @param backend rclone 后端名
// @param root 后端内路径
// @param params 后端参数
// @param onChange 后端写回参数时的持久化回调,可为 nil
// @return 就绪的 Fs
// @return 后端不存在或初始化失败时的错误
func newBackend(ctx context.Context, backend, root string, params map[string]string,
	onChange func(key, value string)) (fs.Fs, error) {
	ri, err := fs.Find(backend)
	if err != nil {
		return nil, fmt.Errorf("未知的 rclone 后端 %q", backend)
	}
	store := &paramStore{values: params, onChange: onChange}
	m := configmap.New()
	m.AddGetter(store, configmap.PriorityNormal)
	m.AddGetter(defaultValues{options: ri.Options}, configmap.PriorityDefault)
	m.AddSetter(store)
	f, err := ri.NewFs(ctx, newName(), root, m)
	if err != nil {
		return nil, fmt.Errorf("初始化 rclone 后端 %q: %w", backend, err)
	}
	return f, nil
}

// newName 生成一个进程内唯一的远端名;只作 rclone 内部标识(日志与 fs 缓存键),不持久化。
// @return 形如 imgyoung-0123abcd 的名字
func newName() string {
	var buf [4]byte
	// 随机源不可用属系统级故障,此处无路径上报;退化为时间戳仍保证唯一。
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("imgyoung-%x", time.Now().UnixNano())
	}
	return "imgyoung-" + hex.EncodeToString(buf[:])
}

// withDefaults 补上图像存储场景的稳妥默认值。
// @Description: S3 兼容后端(R2/MinIO 等)多用作用域凭证,无建桶权限;rclone 上传前默认会
// CreateBucket 探测,桶已存在且无权限时返回 403 让写入直接失败,而只读的健康探测探不到——
// 表现为"健康却写不进"。对接的都是预建桶,故 s3 默认跳过桶检查;调用方显式给值时不覆盖。
// @param backend rclone 后端名
// @param params 后端参数,可为 nil
// @return 补过默认值的新映射(不修改入参)
func withDefaults(backend string, params map[string]string) map[string]string {
	out := maps.Clone(params)
	if out == nil {
		out = map[string]string{}
	}
	if backend == "s3" {
		if _, ok := out["no_check_bucket"]; !ok {
			out["no_check_bucket"] = "true"
		}
	}
	return out
}

// cryptParams 生成 crypt 层参数:内层按登记名引用,密码按 rclone 要求编码。
// @param innerName 内层 Fs 的登记名
// @param c 加密参数
// @return crypt 后端参数
// @return 编码失败时的错误
func cryptParams(innerName string, c *CryptOptions) (map[string]string, error) {
	pw, err := obscure.Obscure(c.Password)
	if err != nil {
		return nil, fmt.Errorf("编码 crypt 密码: %w", err)
	}
	pw2, err := obscure.Obscure(c.Salt)
	if err != nil {
		return nil, fmt.Errorf("编码 crypt 盐: %w", err)
	}
	return map[string]string{
		"remote":              innerName + ":",
		"password":            pw,
		"password2":           pw2,
		"filename_encryption": "standard",
	}, nil
}

// paramStore 后端参数的运行期映射:后端读参经 Get,后端写回(令牌刷新等)经 Set。
type paramStore struct {
	mu       sync.RWMutex            // 后端可能在任意 goroutine 读写参数
	values   map[string]string       // 当前参数
	onChange func(key, value string) // 写回的持久化回调,可为 nil
}

// Get 读取参数;空值视为未设置,交给后端默认值。
// @param key 参数名
// @return 参数值与是否设置
func (p *paramStore) Get(key string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	v, ok := p.values[key]
	return v, ok && v != ""
}

// Set 接收后端写回:更新映射并交持久化回调。
// @Description: 回调在锁外执行——它可能落库,落库期间后端仍可能读参。
// @param key 参数名
// @param value 新值
func (p *paramStore) Set(key, value string) {
	p.mu.Lock()
	p.values[key] = value
	p.mu.Unlock()
	if p.onChange != nil {
		p.onChange(key, value)
	}
}

// defaultValues 以后端选项元数据提供默认值,优先级最低。
type defaultValues struct {
	options fs.Options // 后端声明的选项
}

// Get 读取选项默认值。
// @param key 参数名
// @return 默认值与该选项是否存在
func (d defaultValues) Get(key string) (string, bool) {
	opt := d.options.Get(key)
	if opt == nil {
		return "", false
	}
	return opt.String(), true
}

// Close 释放后端资源:关闭后台任务与连接池。
// @Description: 加密渠道的 crypt 层自身无资源,其 Shutdown 只是转发给内层;内层由 fs
// 缓存项的终结器关闭,故只从缓存移除而不再显式 Shutdown,避免关两次。
// @param ctx 控制关闭的超时与取消
// @return 后端关闭失败时的错误
func (d *Driver) Close(ctx context.Context) error {
	if d.innerName != "" {
		cache.ClearConfig(d.innerName)
		return nil
	}
	if s, ok := d.f.(fs.Shutdowner); ok {
		if err := s.Shutdown(ctx); err != nil {
			return fmt.Errorf("关闭 rclone 后端: %w", err)
		}
	}
	return nil
}

// EnsureBucket 创建渠道根对应的桶/容器,已存在时不报错。
// @Description: 对象存储后端(s3 等)的 Mkdir 在桶层即 CreateBucket。注意:调用方必须
// 用显式 no_check_bucket=false 的实例,否则 s3 后端会短路 Mkdir 使本方法成为空操作。
// 不进 driver.Driver 接口——仅对象存储后端有意义,避免污染核心抽象。
// @return 建桶失败(多为凭证无建桶权限)时的错误
func (d *Driver) EnsureBucket(ctx context.Context) error {
	if err := d.f.Mkdir(ctx, ""); err != nil {
		return fmt.Errorf("创建存储桶: %w", err)
	}
	return nil
}

// Put 将 r 中的 size 个字节流式写入 key。
func (d *Driver) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	src := object.NewStaticObjectInfo(key, time.Now(), size, true, nil, nil)
	if _, err := d.f.Put(ctx, r, src); err != nil {
		return fmt.Errorf("写入对象 %q: %w", key, err)
	}
	return nil
}

// Open 打开 key 对应对象的读取流。
func (d *Driver) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := d.object(ctx, key)
	if err != nil {
		return nil, err
	}
	rc, err := obj.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("打开对象 %q: %w", key, err)
	}
	return rc, nil
}

// Stat 查询 key 对应对象的信息。
func (d *Driver) Stat(ctx context.Context, key string) (*driver.ObjectInfo, error) {
	obj, err := d.object(ctx, key)
	if err != nil {
		return nil, err
	}
	return &driver.ObjectInfo{Key: key, Size: obj.Size(), ModTime: obj.ModTime(ctx)}, nil
}

// Delete 删除 key 对应的对象。
func (d *Driver) Delete(ctx context.Context, key string) error {
	obj, err := d.object(ctx, key)
	if err != nil {
		return err
	}
	if err := obj.Remove(ctx); err != nil {
		return fmt.Errorf("删除对象 %q: %w", key, err)
	}
	return nil
}

// List 递归列举 dir 目录下的全部对象(dir 为空表示整个渠道)。
func (d *Driver) List(ctx context.Context, dir string) ([]driver.ObjectInfo, error) {
	var out []driver.ObjectInfo
	err := walk.ListR(ctx, d.f, dir, false, -1, walk.ListObjects, func(entries fs.DirEntries) error {
		entries.ForObject(func(o fs.Object) {
			out = append(out, driver.ObjectInfo{
				Key:     o.Remote(),
				Size:    o.Size(),
				ModTime: o.ModTime(ctx),
			})
		})
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrorDirNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("列举目录 %q: %w", dir, err)
	}
	return out, nil
}

// object 查找 key 对应的 rclone 对象,并把"不存在"统一映射为 driver.ErrNotFound。
// @Description: rclone 对缺失对象可能返回 ErrorObjectNotFound 或 ErrorDirNotFound
// (取决于缺失层级),对调用方统一收敛为本包哨兵。
func (d *Driver) object(ctx context.Context, key string) (fs.Object, error) {
	obj, err := d.f.NewObject(ctx, key)
	if err != nil {
		if errors.Is(err, fs.ErrorObjectNotFound) || errors.Is(err, fs.ErrorDirNotFound) {
			return nil, fmt.Errorf("对象 %q: %w", key, driver.ErrNotFound)
		}
		return nil, fmt.Errorf("查找对象 %q: %w", key, err)
	}
	return obj, nil
}
