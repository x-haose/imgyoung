import { useEffect, useRef, useState } from "react";
import { Alert, Button, Flex, Input, Segmented, Space, Tag, Typography } from "antd";
import { CheckCircleFilled, LinkOutlined } from "@ant-design/icons";

import { apiFetch } from "../../api/http";

/** 发起授权的响应。 */
interface OAuthStartResponse {
  auth_url: string;
  state: string;
}

/** 领取授权结果的响应。 */
interface OAuthResultResponse {
  pending: boolean;
  token?: string;
  error?: string;
}

/** 授权结果轮询间隔与上限;上限须大于后端会话存活时长,避免先于会话放弃。 */
const POLL_INTERVAL_MS = 1500;
const POLL_TIMEOUT_MS = 11 * 60 * 1000;

interface OAuthFieldProps {
  templateId: string;
  templateName: string;
  /** 已填的其他字段值,提供区域等影响授权端点的参数。 */
  inputs: Record<string, string>;
  /** 当前令牌值;非空表示已授权。 */
  value?: string;
  onChange?: (token: string) => void;
}

/**
 * OAuthField 网盘授权控件:一键授权与粘贴令牌两条路径。
 * 一键授权要求实例已配置对外站点地址并自建应用;粘贴路径不依赖公网可达性,
 * 是无域名部署的唯一可行方式,故并列提供而非二选一。
 */
export function OAuthField({ templateId, templateName, inputs, value, onChange }: OAuthFieldProps) {
  const [mode, setMode] = useState<"oneclick" | "paste">("oneclick");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const cancelled = useRef(false);

  useEffect(() => () => void (cancelled.current = true), []);

  const authorize = async () => {
    setBusy(true);
    setError(null);
    try {
      const start = await apiFetch<OAuthStartResponse>("/api/admin/channel-oauth/start", {
        method: "POST",
        body: JSON.stringify({
          template: templateId,
          inputs,
          client_id: clientId.trim(),
          client_secret: clientSecret.trim(),
        }),
      });
      window.open(start.auth_url, "_blank", "noopener,noreferrer");
      await poll(start.state);
    } catch (err) {
      setError(err instanceof Error ? err.message : "发起授权失败");
    } finally {
      setBusy(false);
    }
  };

  const poll = async (state: string) => {
    const deadline = Date.now() + POLL_TIMEOUT_MS;
    while (Date.now() < deadline) {
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
      if (cancelled.current) return;
      let result: OAuthResultResponse;
      try {
        result = await apiFetch<OAuthResultResponse>(
          `/api/admin/channel-oauth/result?state=${encodeURIComponent(state)}`,
        );
      } catch {
        continue; // 会话尚在,网络抖动不终止轮询
      }
      if (result.error) {
        setError(result.error);
        return;
      }
      if (result.token) {
        onChange?.(result.token);
        return;
      }
    }
    setError("等待授权超时,请重新发起");
  };

  if (value) {
    return (
      <Flex align="center" gap={10}>
        <Tag icon={<CheckCircleFilled />} color="success" style={{ margin: 0, padding: "2px 10px" }}>
          已授权
        </Tag>
        <Button size="small" type="text" onClick={() => onChange?.("")}>
          重新授权
        </Button>
      </Flex>
    );
  }

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Segmented
        value={mode}
        onChange={(v) => setMode(v as typeof mode)}
        options={[
          { label: "一键授权", value: "oneclick" },
          { label: "粘贴令牌", value: "paste" },
        ]}
      />

      {mode === "oneclick" ? (
        <Space direction="vertical" size={8} style={{ width: "100%" }}>
          <Input
            placeholder="应用 Client ID"
            value={clientId}
            onChange={(e) => setClientId(e.target.value)}
            autoComplete="off"
          />
          <Input.Password
            placeholder="应用 Client Secret"
            value={clientSecret}
            onChange={(e) => setClientSecret(e.target.value)}
            autoComplete="off"
          />
          <Button
            type="primary"
            icon={<LinkOutlined />}
            loading={busy}
            disabled={!clientId.trim()}
            onClick={authorize}
          >
            {busy ? "等待授权完成…" : `前往授权 ${templateName}`}
          </Button>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            需在服务商开放平台自建应用,并把本站的
            <Typography.Text code copyable style={{ fontSize: 12 }}>
              /api/admin/channel-oauth/callback
            </Typography.Text>
            登记为回调地址;还需先在设置中填写对外站点地址。
          </Typography.Text>
        </Space>
      ) : (
        <Space direction="vertical" size={8} style={{ width: "100%" }}>
          <Input.TextArea
            rows={3}
            placeholder="粘贴 rclone authorize 的输出"
            onChange={(e) => onChange?.(e.target.value.trim())}
          />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            在任意有浏览器的机器上执行
            <Typography.Text code copyable style={{ fontSize: 12 }}>
              rclone authorize &quot;{templateId}&quot;
            </Typography.Text>
            ,完成授权后把整段输出粘贴到此处。此方式不要求本站有公网域名。
          </Typography.Text>
        </Space>
      )}

      {error && <Alert type="error" showIcon message={error} />}
    </Space>
  );
}
