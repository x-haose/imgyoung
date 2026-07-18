import { BookOutlined, DeleteOutlined, LinkOutlined, UploadOutlined } from "@ant-design/icons";
import { useInvalidate, useList, usePermissions } from "@refinedev/core";
import {
  App as AntdApp,
  Button,
  Card,
  Col,
  Descriptions,
  Drawer,
  Empty,
  Flex,
  Input,
  Pagination,
  Popconfirm,
  Row,
  Space,
  Tag,
  Typography,
  theme,
} from "antd";
import { useState } from "react";
import { Link } from "react-router";

import { apiFetch } from "../../api/http";
import { QueryState } from "../../components/QueryState";
import { usePageClamp } from "../../hooks/usePageClamp";
import { absoluteURL, formatBytes, formatTime } from "../../lib/format";
import { UploadModal } from "./UploadModal";

/** 图片列表项(对应 /api/admin/images 响应)。 */
export interface ImageItem {
  id: string;
  url: string;
  user_id: string;
  token_id: number | null;
  file_name: string;
  mime: string;
  size: number;
  width: number;
  height: number;
  sha256: string;
  expire_at: string | null;
  last_access_at: string | null;
  pending_cleanup: boolean;
  cleanup_exempt: boolean;
  created_at: string;
  ref_count?: number;
}

/** 每页卡片数(4 列 × 6 行)。 */
const pageSize = 24;

/** 图片页:缩略卡片网格 + 搜索过滤 + 详情抽屉 + 删除。 */
export function ImagesPage() {
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const [active, setActive] = useState<ImageItem | null>(null);
  const [uploadOpen, setUploadOpen] = useState(false);
  const { data: role } = usePermissions<string>({});
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();
  const { token } = theme.useToken();

  const list = useList<ImageItem>({
    resource: "images",
    pagination: { currentPage: page, pageSize },
    filters: search ? [{ field: "search", operator: "eq", value: search }] : [],
  });
  const items = list.result?.data ?? [];
  const total = list.result?.total ?? 0;
  usePageClamp(page, setPage, list.result?.total, pageSize);

  const refresh = () => invalidate({ resource: "images", invalidates: ["list"] });

  const removeImage = async (id: string) => {
    try {
      await apiFetch(`/api/admin/images/${id}`, { method: "DELETE" });
      message.success("已删除");
      setActive(null);
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "删除失败");
    }
  };

  return (
    <Flex vertical gap={16}>
      <Flex align="center" justify="space-between" wrap gap={12}>
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            图片
          </Typography.Title>
          <Typography.Text type="secondary">
            {role === "admin" ? "全站图片" : "我的图片"} · 共 {total} 张
          </Typography.Text>
        </div>
        <Space wrap>
          <Input.Search
            allowClear
            placeholder="按短 ID / 文件名搜索"
            style={{ width: 280 }}
            onSearch={(v) => {
              setPage(1);
              setSearch(v.trim());
            }}
          />
          <Button type="primary" icon={<UploadOutlined />} onClick={() => setUploadOpen(true)}>
            上传图片
          </Button>
        </Space>
      </Flex>

      <QueryState query={list.query}>
        {items.length === 0 ? (
          search ? (
            <Empty description="没有匹配的图片" style={{ padding: "64px 0" }} />
          ) : (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              style={{ padding: "64px 0" }}
              description={
                <Flex vertical gap={4}>
                  <Typography.Text strong>还没有图片</Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                    从这里直接上传,或用 PicGo / curl 等客户端带 Token 上传
                  </Typography.Text>
                </Flex>
              }
            >
              <Space wrap>
                <Button type="primary" icon={<UploadOutlined />} onClick={() => setUploadOpen(true)}>
                  上传第一张图
                </Button>
                <Link to="/guide">
                  <Button icon={<BookOutlined />}>查看使用指南</Button>
                </Link>
              </Space>
            </Empty>
          )
        ) : (
          <Row gutter={[16, 16]}>
            {items.map((img) => (
              <Col key={img.id} xs={12} sm={8} md={6} xl={4}>
                <Card
                  hoverable
                  onClick={() => setActive(img)}
                  styles={{ body: { padding: "10px 12px" } }}
                  cover={
                    <div
                      style={{
                        aspectRatio: "4 / 3",
                        overflow: "hidden",
                        background: token.colorBgLayout,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                      }}
                    >
                      <img
                        src={`${img.url}?w=400`}
                        alt={img.file_name || img.id}
                        loading="lazy"
                        style={{ width: "100%", height: "100%", objectFit: "cover" }}
                      />
                    </div>
                  }
                >
                  <Typography.Text ellipsis style={{ display: "block", fontSize: 12 }}>
                    {img.file_name || img.id}
                  </Typography.Text>
                  <Flex justify="space-between" align="center" style={{ marginTop: 2 }}>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {formatBytes(img.size)}
                    </Typography.Text>
                    {img.pending_cleanup && <Tag color="warning">待清理</Tag>}
                  </Flex>
                </Card>
              </Col>
            ))}
          </Row>
        )}

        <Flex justify="center">
          <Pagination
            current={page}
            pageSize={pageSize}
            total={total}
            showSizeChanger={false}
            onChange={setPage}
            hideOnSinglePage
          />
        </Flex>
      </QueryState>

      <Drawer
        open={active !== null}
        onClose={() => setActive(null)}
        width={420}
        title={active?.file_name || active?.id}
        extra={
          active && (
            <Popconfirm
              title="删除这张图片?"
              description="引用即刻删除,物理文件按宽限期级联清理。"
              okButtonProps={{ danger: true }}
              onConfirm={() => removeImage(active.id)}
            >
              <Button danger icon={<DeleteOutlined />}>
                删除
              </Button>
            </Popconfirm>
          )
        }
      >
        {active && (
          <Flex vertical gap={16}>
            <a href={active.url} target="_blank" rel="noreferrer">
              <img
                src={`${active.url}?w=800`}
                alt={active.file_name || active.id}
                style={{ width: "100%", borderRadius: 8 }}
              />
            </a>
            <Descriptions
              column={1}
              size="small"
              items={[
                {
                  label: "链接",
                  children: (
                    <Typography.Text copyable={{ text: absoluteURL(active.url) }}>
                      <LinkOutlined /> {active.url}
                    </Typography.Text>
                  ),
                },
                { label: "格式", children: `${active.mime} · ${active.width}×${active.height}` },
                { label: "大小", children: formatBytes(active.size) },
                { label: "归属", children: active.user_id },
                { label: "上传时间", children: formatTime(active.created_at) },
                { label: "最近访问", children: formatTime(active.last_access_at) },
                {
                  label: "到期",
                  children: active.expire_at ? formatTime(active.expire_at) : "永不过期",
                },
                {
                  label: "SHA-256",
                  children: (
                    <Typography.Text code copyable style={{ fontSize: 11 }}>
                      {active.sha256}
                    </Typography.Text>
                  ),
                },
              ]}
            />
          </Flex>
        )}
      </Drawer>

      <UploadModal open={uploadOpen} onClose={() => setUploadOpen(false)} onUploaded={refresh} />
    </Flex>
  );
}
