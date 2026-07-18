import { useInvalidate, useList } from "@refinedev/core";
import {
  App as AntdApp,
  Button,
  Card,
  Empty,
  Flex,
  Image,
  Popconfirm,
  Table,
  Typography,
} from "antd";
import { useState } from "react";

import { apiFetch } from "../../api/http";
import { QueryState } from "../../components/QueryState";
import { usePageClamp } from "../../hooks/usePageClamp";
import { formatBytes, formatTime } from "../../lib/format";

/** 待清理项(对应 /api/admin/cleanup 响应)。 */
interface CleanupItem {
  id: string;
  url: string;
  user_id: string;
  file_name: string;
  mime: string;
  size: number;
  width: number;
  height: number;
  last_access_at: string | null;
  created_at: string;
}

/** 每页冷图条数。 */
const pageSize = 50;

/** 待清理页:冷图列表,裁决保留(豁免)或删除。 */
export function CleanupPage() {
  const [page, setPage] = useState(1);
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();

  const list = useList<CleanupItem>({
    resource: "cleanup",
    pagination: { currentPage: page, pageSize },
  });
  const items = list.result?.data ?? [];
  const total = list.result?.total ?? 0;
  usePageClamp(page, setPage, list.result?.total, pageSize);
  // 裁决同时影响图片列表(删/保留都动图片),一并失效使相邻页不显示陈旧数据。
  const refresh = () => {
    invalidate({ resource: "cleanup", invalidates: ["list"] });
    invalidate({ resource: "images", invalidates: ["list"] });
  };

  const keep = async (id: string) => {
    try {
      await apiFetch(`/api/admin/cleanup/${id}/keep`, { method: "POST" });
      message.success("已保留,该图不再参与冷图打标");
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "操作失败");
    }
  };

  const remove = async (id: string) => {
    try {
      await apiFetch(`/api/admin/images/${id}`, { method: "DELETE" });
      message.success("已删除");
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "删除失败");
    }
  };

  return (
    <Flex vertical gap={16}>
      <div>
        <Typography.Title level={4} style={{ margin: 0 }}>
          待清理
        </Typography.Title>
        <Typography.Text type="secondary">
          超期未被公开访问的冷图,共 {total} 张;不自动删,由你裁决
        </Typography.Text>
      </div>

      <QueryState query={list.query}>
        <Card styles={{ body: { padding: 0 } }}>
          <Table<CleanupItem>
            rowKey="id"
            dataSource={items}
            loading={list.query.isFetching}
            pagination={{
              current: page,
              pageSize,
              total,
              showSizeChanger: false,
              onChange: setPage,
            }}
            scroll={{ x: 880 }}
            size="middle"
            locale={{ emptyText: <Empty description="没有待清理的冷图" /> }}
            columns={[
              {
                title: "预览",
                key: "preview",
                width: 84,
                render: (_, row) => (
                  <Image
                    src={`${row.url}?w=100`}
                    alt={row.file_name || row.id}
                    width={56}
                    height={42}
                    style={{ objectFit: "cover", borderRadius: 6 }}
                    preview={{ src: row.url }}
                  />
                ),
              },
              {
                title: "图片",
                key: "name",
                render: (_, row) => (
                  <Flex vertical>
                    <Typography.Text>{row.file_name || row.id}</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {row.mime} · {row.width}×{row.height} · {formatBytes(row.size)}
                    </Typography.Text>
                  </Flex>
                ),
              },
              { title: "归属", dataIndex: "user_id", width: 120 },
              {
                title: "最近访问",
                dataIndex: "last_access_at",
                render: (v: string | null) => (v ? formatTime(v) : "从未访问"),
              },
              { title: "上传时间", dataIndex: "created_at", render: formatTime },
              {
                title: "裁决",
                key: "actions",
                width: 160,
                render: (_, row) => (
                  <Flex gap={4}>
                    <Button size="small" onClick={() => keep(row.id)}>
                      保留
                    </Button>
                    <Popconfirm
                      title="删除这张冷图?"
                      okButtonProps={{ danger: true }}
                      onConfirm={() => remove(row.id)}
                    >
                      <Button size="small" danger>
                        删除
                      </Button>
                    </Popconfirm>
                  </Flex>
                ),
              },
            ]}
          />
        </Card>
      </QueryState>
    </Flex>
  );
}
