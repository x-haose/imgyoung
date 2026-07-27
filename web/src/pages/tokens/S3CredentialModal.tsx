import { CloudServerOutlined } from "@ant-design/icons";
import { Alert, Descriptions, Modal, Typography } from "antd";

/** S3 凭证与端点配置(对应 /api/admin/tokens/:id/s3-credential 响应)。 */
export interface S3Credential {
  access_key_id: string;
  secret_access_key: string;
  endpoint: string;
  region: string;
  bucket: string;
  url_prefix: string;
}

interface S3CredentialModalProps {
  credential: S3Credential | null;
  tokenName: string;
  onClose: () => void;
}

/** 配置项在弹窗里的呈现顺序,与 PicGo「Amazon S3」表单字段一一对应,照抄即可。 */
const fieldOrder: { key: keyof S3Credential; label: string; hint?: string }[] = [
  { key: "access_key_id", label: "应用密钥 ID" },
  { key: "secret_access_key", label: "应用密钥" },
  { key: "bucket", label: "桶名", hint: "本服务无桶概念,填什么都收" },
  { key: "region", label: "区域", hint: "不区分区域,保持默认即可" },
  { key: "endpoint", label: "自定义节点", hint: "末尾的 /s3 是协议面前缀,不能省" },
  { key: "url_prefix", label: "自定义域名", hint: "客户端据此拼最终图片地址,含用户 id 段" },
];

/**
 * S3CredentialModal 展示一枚 Token 的 S3 协议凭证,供 PicGo 等客户端照填。
 * 与 Token 明文弹窗刻意不同:密钥由主密钥按需派生、不落库,故可随时重看,
 * 不必为丢失密钥而轮转 Token,也就不需要「仅此一次」的防误关处理。
 */
export function S3CredentialModal({ credential, tokenName, onClose }: S3CredentialModalProps) {
  return (
    <Modal
      open={credential !== null}
      title={
        <>
          <CloudServerOutlined /> S3 客户端配置 · {tokenName}
        </>
      }
      onCancel={onClose}
      footer={null}
      width={620}
    >
      <Alert
        type="info"
        showIcon
        message="在 PicGo / PicList / Typora 里选「Amazon S3」,按下表填写即可"
        description="密钥由主密钥派生,可随时回此页重看;吊销该 Token 即同步失效。"
        style={{ marginBottom: 16 }}
      />
      <Descriptions column={1} size="small" bordered>
        {fieldOrder.map(({ key, label, hint }) => (
          <Descriptions.Item key={key} label={label}>
            <Typography.Text copyable code style={{ wordBreak: "break-all" }}>
              {credential?.[key]}
            </Typography.Text>
            {hint && (
              <Typography.Text
                type="secondary"
                style={{ display: "block", fontSize: 12, marginTop: 4 }}
              >
                {hint}
              </Typography.Text>
            )}
          </Descriptions.Item>
        ))}
      </Descriptions>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 12, marginBottom: 0 }}>
        路径风格寻址(path-style):自部署没有桶子域名,客户端若有「使用路径风格」开关请勾上。
      </Typography.Paragraph>
    </Modal>
  );
}
