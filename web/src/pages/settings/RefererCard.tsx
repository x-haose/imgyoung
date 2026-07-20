import { PlusOutlined } from "@ant-design/icons";
import { Alert, App as AntdApp, Button, Card, Flex, Input, Popconfirm, Space, Tag, Typography } from "antd";
import { useCallback, useEffect, useState } from "react";

import { apiFetch } from "../../api/http";

/** 防盗链白名单规则行(GET /api/admin/referer-whitelist)。 */
interface RefererRule {
  id: number; // 规则行 id,删除用
  pattern: string; // 规则:裸域或子域通配
  created_at: string; // 添加时刻
}

/** RefererCard 的属性。 */
interface RefererCardProps {
  allowEmptyReferer?: boolean; // 空 Referer 放行策略(展示用);undefined 表示配置尚未加载
}

/** 防盗链卡片:规则列表 + 增删(即时生效),空 Referer 策略展示(仅管理员)。 */
export function RefererCard({ allowEmptyReferer }: RefererCardProps) {
  const [rules, setRules] = useState<RefererRule[] | undefined>(undefined);
  const [loadError, setLoadError] = useState(false);
  const [pattern, setPattern] = useState("");
  const [adding, setAdding] = useState(false);
  const [removingId, setRemovingId] = useState<number | null>(null);
  const { message } = AntdApp.useApp();

  const load = useCallback(async () => {
    try {
      const resp = await apiFetch<{ items: RefererRule[] }>("/api/admin/referer-whitelist");
      setRules(resp.items);
      setLoadError(false);
    } catch (err) {
      // 拉取失败不等于未启用:置错误态而非空列表,避免误报「防盗链已关闭」。
      message.error(err instanceof Error ? err.message : "加载防盗链规则失败");
      setLoadError(true);
    }
  }, [message]);

  useEffect(() => {
    void load();
  }, [load]);

  const add = async () => {
    if (!pattern.trim()) {
      return;
    }
    setAdding(true);
    try {
      await apiFetch("/api/admin/referer-whitelist", {
        method: "POST",
        body: JSON.stringify({ pattern }),
      });
      setPattern("");
      message.success("规则已添加,即时生效");
      await load();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "添加失败");
    } finally {
      setAdding(false);
    }
  };

  const remove = async (rule: RefererRule) => {
    try {
      await apiFetch(`/api/admin/referer-whitelist/${rule.id}`, { method: "DELETE" });
      message.success(`已删除 ${rule.pattern}`);
      await load();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "删除失败");
    }
  };

  return (
    <Card title="防盗链" loading={rules === undefined && !loadError} style={{ height: "100%" }}>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 16 }}>
        仅命中白名单的 Referer 才能访问公开图片链接(/i/);规则增删即时生效,无需重启。
        空 Referer{allowEmptyReferer === false ? "拒绝" : "放行"}
        (在「运行配置 · 安全」修改,同样即时生效)。
      </Typography.Paragraph>
      {loadError ? (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 16 }}
          message="防盗链规则加载失败,无法确认当前防盗链状态"
          action={
            <Button size="small" onClick={() => void load()}>
              重试
            </Button>
          }
        />
      ) : rules && rules.length > 0 ? (
        <Flex wrap gap={8} style={{ marginBottom: 16 }}>
          {rules.map((rule) => (
            <Popconfirm
              key={rule.id}
              title={`删除规则 ${rule.pattern}?`}
              description="删除后即时生效,对应 Referer 将按剩余规则执法。"
              okButtonProps={{ danger: true }}
              open={removingId === rule.id}
              onConfirm={() => {
                setRemovingId(null);
                void remove(rule);
              }}
              onCancel={() => setRemovingId(null)}
            >
              <Tag
                closable
                onClose={(e) => {
                  e.preventDefault(); // 删除以接口结果为准,不做乐观移除;先弹确认
                  setRemovingId(rule.id);
                }}
                style={{ fontFamily: "monospace", padding: "2px 8px", marginInlineEnd: 0 }}
              >
                {rule.pattern}
              </Tag>
            </Popconfirm>
          ))}
        </Flex>
      ) : (
        <Typography.Paragraph type="secondary" style={{ marginBottom: 16 }}>
          未启用防盗链,所有 Referer 均可访问。
        </Typography.Paragraph>
      )}
      <Space.Compact style={{ width: "100%", maxWidth: 360 }}>
        <Input
          value={pattern}
          onChange={(e) => setPattern(e.target.value)}
          onPressEnter={() => void add()}
          placeholder="*.example.com 或 example.com"
          allowClear
        />
        <Button type="primary" icon={<PlusOutlined />} loading={adding} onClick={() => void add()}>
          添加
        </Button>
      </Space.Compact>
    </Card>
  );
}
