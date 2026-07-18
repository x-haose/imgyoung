import { ClockCircleOutlined, DeleteOutlined, QuestionCircleOutlined } from "@ant-design/icons";
import {
  App as AntdApp,
  Button,
  Descriptions,
  Drawer,
  Flex,
  InputNumber,
  Popconfirm,
  Popover,
  Space,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import { usePermissions } from "@refinedev/core";
import { useState } from "react";

import { apiFetch } from "../../api/http";
import { copyText } from "../../lib/clipboard";
import { absoluteURL, formatBytes, formatTime } from "../../lib/format";
import type { ImageItem } from "./types";

/** 删除确认的统一文案(单删/批删一致,不承诺不存在的宽限)。 */
export const deleteWarning = "删除后外链立即失效,该操作不可恢复";

/** ImageDetailDrawer 的属性。 */
interface ImageDetailDrawerProps {
  image: ImageItem | null; // 当前展示的图片;null 关闭抽屉
  onClose: () => void; // 请求关闭
  onDeleted: (id: string) => void; // 删除成功(调用方刷新列表并关抽屉)
  onPatched: (updated: ImageItem) => void; // 修改成功(调用方更新抽屉数据并刷新列表)
}

/** 复制格式定义:label → 由绝对链接生成文本。 */
const copyFormats: { label: string; build: (url: string) => string }[] = [
  { label: "URL", build: (url) => url },
  { label: "Markdown", build: (url) => `![](${url})` },
  { label: "HTML", build: (url) => `<img src="${url}" />` },
  { label: "BBCode", build: (url) => `[img]${url}[/img]` },
];

/** 图片详情抽屉:预览 + 多格式复制 + 元数据 + 有效期/豁免管理 + 删除。 */
export function ImageDetailDrawer({ image, onClose, onDeleted, onPatched }: ImageDetailDrawerProps) {
  const [extendDays, setExtendDays] = useState<number | null>(7);
  const [patching, setPatching] = useState(false);
  const { message } = AntdApp.useApp();
  const { data: role } = usePermissions<string>({});
  const isAdmin = role === "admin"; // 清理豁免属管理员裁决权,属主只读展示

  /** copy 复制指定格式并提示。 */
  const copy = async (label: string, text: string) => {
    if (await copyText(text)) {
      message.success(`${label} 已复制`);
    } else {
      message.error("复制失败,请手动复制");
    }
  };

  /** patch 提交单项修改并把后端回读的最新详情交给父级。 */
  const patch = async (body: { ttl_hours?: number; cleanup_exempt?: boolean }, done: string) => {
    if (!image) {
      return;
    }
    setPatching(true);
    try {
      const updated = await apiFetch<ImageItem>(`/api/admin/images/${image.id}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      });
      message.success(done);
      onPatched(updated);
    } catch (err) {
      message.error(err instanceof Error ? err.message : "修改失败");
    } finally {
      setPatching(false);
    }
  };

  /** remove 删除当前图片并通知父级。 */
  const remove = async () => {
    if (!image) {
      return;
    }
    try {
      await apiFetch(`/api/admin/images/${image.id}`, { method: "DELETE" });
      message.success("已删除");
      onDeleted(image.id);
    } catch (err) {
      message.error(err instanceof Error ? err.message : "删除失败");
    }
  };

  const ttlPanel = (
    <Flex vertical gap={10} style={{ width: 220, paddingTop: 4 }}>
      <Space.Compact style={{ width: "100%" }}>
        <InputNumber
          min={1}
          precision={0}
          value={extendDays}
          onChange={setExtendDays}
          suffix="天"
          style={{ flex: 1 }}
        />
        <Button
          type="primary"
          loading={patching}
          disabled={extendDays == null || extendDays < 1}
          onClick={() =>
            void patch({ ttl_hours: (extendDays ?? 0) * 24 }, `已延长至 ${extendDays} 天后`)
          }
        >
          延长
        </Button>
      </Space.Compact>
      <Button
        block
        loading={patching}
        disabled={image?.expire_at == null}
        onClick={() => void patch({ ttl_hours: 0 }, "已设为永久")}
      >
        设为永久
      </Button>
    </Flex>
  );

  return (
    <Drawer
      open={image !== null}
      onClose={onClose}
      width={420}
      title={image?.file_name || image?.id}
      extra={
        image && (
          <Popconfirm
            title="删除这张图片?"
            description={deleteWarning}
            okButtonProps={{ danger: true }}
            onConfirm={() => void remove()}
          >
            <Button danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        )
      }
    >
      {image && (
        <Flex vertical gap={16}>
          <a href={image.url} target="_blank" rel="noreferrer">
            <img
              src={`${image.url}?w=800`}
              alt={image.file_name || image.id}
              style={{ width: "100%", borderRadius: 8 }}
            />
          </a>

          <div>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              复制链接
            </Typography.Text>
            <Space.Compact block style={{ marginTop: 6 }}>
              {copyFormats.map((f) => (
                <Button
                  key={f.label}
                  size="small"
                  style={{ flex: 1 }}
                  onClick={() => void copy(f.label, f.build(absoluteURL(image.url)))}
                >
                  {f.label}
                </Button>
              ))}
            </Space.Compact>
          </div>

          <Descriptions
            column={1}
            size="small"
            items={[
              { label: "格式", children: `${image.mime} · ${image.width}×${image.height}` },
              { label: "大小", children: formatBytes(image.size) },
              { label: "归属", children: image.user_id },
              { label: "来源", children: image.token_name ?? "后台上传 / 来源已删除" },
              {
                label: "引用数",
                children: (
                  <Space size={4}>
                    {image.ref_count}
                    <Tooltip title="同一物理文件被几个引用共享(查重合并所致);删除最后一个引用才会清理物理文件">
                      <QuestionCircleOutlined style={{ fontSize: 12, opacity: 0.55 }} />
                    </Tooltip>
                  </Space>
                ),
              },
              { label: "上传时间", children: formatTime(image.created_at) },
              { label: "最近访问", children: formatTime(image.last_access_at) },
              {
                label: "到期",
                children: (
                  <Space size={6} wrap>
                    {image.expire_at ? formatTime(image.expire_at) : "永不过期"}
                    {image.pending_cleanup && <Tag color="warning">待清理</Tag>}
                    <Popover trigger="click" placement="bottomLeft" content={ttlPanel}>
                      <Button size="small" type="link" icon={<ClockCircleOutlined />} style={{ padding: 0 }}>
                        修改有效期
                      </Button>
                    </Popover>
                  </Space>
                ),
              },
              {
                label: "清理豁免",
                children: (
                  <Space size={6}>
                    {isAdmin ? (
                      <Switch
                        size="small"
                        checked={image.cleanup_exempt}
                        loading={patching}
                        onChange={(checked) =>
                          void patch({ cleanup_exempt: checked }, checked ? "已设为豁免" : "已取消豁免")
                        }
                      />
                    ) : (
                      <Typography.Text style={{ fontSize: 13 }}>
                        {image.cleanup_exempt ? "已豁免" : "未豁免"}
                      </Typography.Text>
                    )}
                    <Tooltip title="豁免后不参与冷图打标,TTL 到期与访问续期不受影响;仅管理员可裁决,取消豁免由下轮扫描重判">
                      <QuestionCircleOutlined style={{ fontSize: 12, opacity: 0.55 }} />
                    </Tooltip>
                  </Space>
                ),
              },
              {
                label: "SHA-256",
                children: (
                  <Typography.Text code copyable style={{ fontSize: 11 }}>
                    {image.sha256}
                  </Typography.Text>
                ),
              },
            ]}
          />
        </Flex>
      )}
    </Drawer>
  );
}
