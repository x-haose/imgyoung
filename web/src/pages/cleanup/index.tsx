import { useInvalidate, useList } from "@refinedev/core";
import {
  Alert,
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
import { useState, type Key } from "react";

import { apiFetch } from "../../api/http";
import { QueryState } from "../../components/QueryState";
import { usePageClamp } from "../../hooks/usePageClamp";
import { useServerConfig } from "../../hooks/useServerConfig";
import { adminThumbSrc, deleteWarningText, formatBytes, formatTime } from "../../lib/format";
import { batchDeleteMax, type BatchDeleteResult } from "../images/types";

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

/** 批量保留的确认说明(与单条保留同规格:保留即豁免,也需明确后果)。 */
const keepWarning = "豁免后不再参与冷图打标,可在图片详情中取消";

/** 每页冷图条数。 */
const pageSize = 50;

/** 待清理页:冷图列表,单条或批量裁决保留(豁免)/删除。 */
export function CleanupPage() {
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Key[]>([]);
  const [batching, setBatching] = useState(false);
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();
  const serverConfig = useServerConfig();

  const list = useList<CleanupItem>({
    resource: "cleanup",
    pagination: { currentPage: page, pageSize },
  });
  const items = list.result?.data ?? [];
  const total = list.result?.total ?? 0;
  usePageClamp(page, setPage, list.result?.total, pageSize);
  // 裁决同时影响图片列表(删/保留都动图片),一并失效使相邻页不显示陈旧数据。
  const refresh = () => {
    setSelected([]);
    invalidate({ resource: "cleanup", invalidates: ["list"] });
    invalidate({ resource: "images", invalidates: ["list"] });
  };

  const keep = async (id: string) => {
    try {
      await apiFetch(`/api/admin/images/${id}`, {
        method: "PATCH",
        body: JSON.stringify({ cleanup_exempt: true }),
      });
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

  /** batchKeep 逐条保留已选(豁免是单图 PATCH,顺序循环;失败条目计数后继续)。 */
  const batchKeep = async () => {
    setBatching(true);
    let kept = 0;
    let failed = 0;
    for (const id of selected) {
      try {
        await apiFetch(`/api/admin/images/${String(id)}`, {
          method: "PATCH",
          body: JSON.stringify({ cleanup_exempt: true }),
        });
        kept++;
      } catch {
        failed++;
      }
    }
    setBatching(false);
    if (failed > 0) {
      message.warning(`已保留 ${kept} 张,${failed} 张失败`);
    } else {
      message.success(`已保留 ${kept} 张,不再参与冷图打标`);
    }
    refresh();
  };

  /** batchRemove 批量删除已选(与图片页共用 batch-delete 端点)。 */
  const batchRemove = async () => {
    setBatching(true);
    try {
      const res = await apiFetch<BatchDeleteResult>("/api/admin/images/batch-delete", {
        method: "POST",
        body: JSON.stringify({ ids: selected.map(String) }),
      });
      if (res.failed.length > 0) {
        message.warning(`已删除 ${res.deleted} 张,${res.failed.length} 张失败:${res.failed[0].error}`);
      } else {
        message.success(`已删除 ${res.deleted} 张`);
      }
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "批量删除失败");
    } finally {
      setBatching(false);
    }
  };

  const coldDays = serverConfig?.cold_cleanup_days;
  return (
    <Flex vertical gap={16}>
      <Flex align="center" justify="space-between" wrap gap={12}>
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            待清理
          </Typography.Title>
          <Typography.Text type="secondary">
            {coldDays
              ? `超过 ${coldDays} 天无公开访问的图片会进入此列表,`
              : "超期未被公开访问的冷图,"}
            共 {total} 张;不自动删,由你裁决
          </Typography.Text>
        </div>
        {selected.length > 0 && (
          <Flex gap={8}>
            <Popconfirm
              title={`保留选中的 ${selected.length} 张图片?`}
              description={keepWarning}
              onConfirm={() => void batchKeep()}
            >
              <Button loading={batching}>批量保留</Button>
            </Popconfirm>
            <Popconfirm
              title={`删除选中的 ${selected.length} 张图片?`}
              description={deleteWarningText(serverConfig?.delete_grace_minutes)}
              okButtonProps={{ danger: true }}
              onConfirm={() => void batchRemove()}
            >
              <Button danger loading={batching}>
                批量删除
              </Button>
            </Popconfirm>
          </Flex>
        )}
      </Flex>

      {coldDays === 0 && (
        <Alert
          type="info"
          showIcon
          message="冷图打标未启用(cold_cleanup_days=0),此列表不会有新条目"
        />
      )}

      <QueryState query={list.query}>
        <Card styles={{ body: { padding: 0 } }}>
          <Table<CleanupItem>
            rowKey="id"
            dataSource={items}
            loading={list.query.isFetching}
            rowSelection={{
              selectedRowKeys: selected,
              // 与批量删除端点的上限对齐:跨页勾选超限时截断并提示,避免整请求被 400 拒绝。
              onChange: (keys) => {
                if (keys.length > batchDeleteMax) {
                  message.warning(`一次最多选择 ${batchDeleteMax} 张`);
                  setSelected(keys.slice(0, batchDeleteMax));
                  return;
                }
                setSelected(keys);
              },
            }}
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
                    src={adminThumbSrc(row.id, serverConfig?.process_widths, 100)}
                    alt={row.file_name || row.id}
                    width={56}
                    height={42}
                    style={{ objectFit: "cover", borderRadius: 6 }}
                    // 大图预览也走不计数通道:冷图正因"无访问"才在此列表,
                    // 走 /i/ 会把它当公开访问摘出待裁决队列。
                    preview={{ src: `/api/admin/images/${row.id}/thumb` }}
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
                    <Popconfirm
                      title="保留这张冷图?"
                      description={keepWarning}
                      onConfirm={() => void keep(row.id)}
                    >
                      <Button size="small">保留</Button>
                    </Popconfirm>
                    <Popconfirm
                      title="删除这张冷图?"
                      description={deleteWarningText(serverConfig?.delete_grace_minutes)}
                      okButtonProps={{ danger: true }}
                      onConfirm={() => void remove(row.id)}
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
