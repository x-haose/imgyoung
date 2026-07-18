import { PlusOutlined } from "@ant-design/icons";
import { useInvalidate, useList } from "@refinedev/core";
import {
  App as AntdApp,
  Button,
  Card,
  Empty,
  Flex,
  Popconfirm,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  theme,
} from "antd";
import { useState } from "react";

import { apiFetch } from "../../api/http";
import { QueryState } from "../../components/QueryState";
import { formatRelativeTime, formatTime } from "../../lib/format";
import { ChannelFormModal } from "./ChannelFormModal";
import type { ChannelItem, ReplicaStats } from "./types";

/** 渠道页:列表(健康详情 + 副本水位)+ 模板化创建/编辑 + 手动重检 + 删除。 */
export function ChannelsPage() {
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<ChannelItem | null>(null);
  const [checkingId, setCheckingId] = useState<number | null>(null);
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();

  const list = useList<ChannelItem>({
    resource: "channels",
    pagination: { currentPage: 1, pageSize: 100 },
  });
  const items = list.result?.data ?? [];
  const refresh = () => invalidate({ resource: "channels", invalidates: ["list"] });

  const openCreate = () => {
    setEditing(null);
    setFormOpen(true);
  };

  const openEdit = (row: ChannelItem) => {
    setEditing(row);
    setFormOpen(true);
  };

  const remove = async (id: number) => {
    try {
      await apiFetch(`/api/admin/channels/${id}`, { method: "DELETE" });
      message.success("渠道已删除");
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "删除失败");
    }
  };

  // 手动重检:深探测后按返回的最新描述反馈结果,列表随刷新更新该行。
  const check = async (row: ChannelItem) => {
    setCheckingId(row.id);
    try {
      const info = await apiFetch<ChannelItem>(`/api/admin/channels/${row.id}/check`, {
        method: "POST",
      });
      if (info.last_error) {
        message.warning(`渠道 ${info.name} 探测失败:${info.last_error}`, 6);
      } else {
        message.success(`渠道 ${info.name} 探测通过`);
      }
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "重检失败");
    } finally {
      setCheckingId(null);
    }
  };

  return (
    <Flex vertical gap={16}>
      <Flex align="center" justify="space-between">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            存储渠道
          </Typography.Title>
          <Typography.Text type="secondary">
            多渠道主备容灾;优先级最高的健康渠道承接上传主写
          </Typography.Text>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          新建渠道
        </Button>
      </Flex>

      <QueryState query={list.query}>
        <Card styles={{ body: { padding: 0 } }}>
          <Table<ChannelItem>
            rowKey="id"
            dataSource={items}
            loading={list.query.isFetching}
            pagination={false}
            scroll={{ x: 760 }}
            size="middle"
            locale={{
              emptyText: (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  style={{ padding: "40px 0" }}
                  description={
                    <Flex vertical gap={4}>
                      <Typography.Text strong>还没有存储渠道</Typography.Text>
                      <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                        先新建一个,上传的图片才有落脚点
                      </Typography.Text>
                    </Flex>
                  }
                >
                  <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                    新建渠道
                  </Button>
                </Empty>
              ),
            }}
            columns={[
              { title: "渠道", dataIndex: "name" },
              { title: "驱动", dataIndex: "driver", render: (v: string) => <Tag>{v}</Tag> },
              { title: "优先级", dataIndex: "priority", align: "right", width: 80 },
              {
                title: "状态",
                key: "healthy",
                width: 120,
                render: (_, row) => <HealthCell row={row} />,
              },
              {
                title: "副本",
                key: "replicas",
                width: 180,
                render: (_, row) => <ReplicaCell stats={row.replicas} />,
              },
              {
                title: "特性",
                key: "flags",
                render: (_, row) => (
                  <Space size={4}>
                    {row.encrypted && <Tag color="geekblue">加密</Tag>}
                    {row.allow_direct_link && <Tag color="cyan">可直链</Tag>}
                  </Space>
                ),
              },
              {
                title: "操作",
                key: "actions",
                width: 170,
                render: (_, row) => (
                  <Space size={0}>
                    <Button size="small" type="text" onClick={() => openEdit(row)}>
                      编辑
                    </Button>
                    <Button
                      size="small"
                      type="text"
                      loading={checkingId === row.id}
                      onClick={() => check(row)}
                    >
                      重检
                    </Button>
                    <Popconfirm
                      title={`删除渠道 ${row.name}?`}
                      description="持有唯一副本的渠道会被拒绝删除,防止丢数据。"
                      okButtonProps={{ danger: true }}
                      onConfirm={() => remove(row.id)}
                    >
                      <Button size="small" danger type="text">
                        删除
                      </Button>
                    </Popconfirm>
                  </Space>
                ),
              },
            ]}
          />
        </Card>
      </QueryState>

      <ChannelFormModal
        open={formOpen}
        channel={editing}
        onClose={() => setFormOpen(false)}
        onSaved={() => {
          setFormOpen(false);
          refresh();
        }}
      />
    </Flex>
  );
}

/** HealthCell 状态列:健康/异常 Tag(异常带失败原因),下方小字显示最近探测的相对时间。 */
function HealthCell({ row }: { row: ChannelItem }) {
  return (
    <Flex vertical gap={2} align="flex-start">
      {row.healthy ? (
        <Tag color="success">健康</Tag>
      ) : (
        <Tooltip title={row.last_error || "探测未通过"}>
          <Tag color="error" style={{ cursor: "help" }}>
            异常
          </Tag>
        </Tooltip>
      )}
      {row.last_checked_at === "" ? (
        <Typography.Text type="secondary" style={{ fontSize: 11 }}>
          未探测
        </Typography.Text>
      ) : (
        <Tooltip title={`最近探测:${formatTime(row.last_checked_at)}`}>
          <Typography.Text type="secondary" style={{ fontSize: 11, cursor: "default" }}>
            {formatRelativeTime(row.last_checked_at)}探测
          </Typography.Text>
        </Tooltip>
      )}
    </Flex>
  );
}

/** ReplicaCell 副本水位:「N 就绪 · N 待补 · N 失败」带标注紧凑展示,非零 pending/failed 着色,Tooltip 补充细节。 */
function ReplicaCell({ stats }: { stats: ReplicaStats }) {
  const { token } = theme.useToken();
  const num = (n: number, label: string, tip: string, activeColor: string) => (
    <Tooltip title={tip}>
      <span
        style={{
          color: n > 0 ? activeColor : token.colorTextQuaternary,
          fontVariantNumeric: "tabular-nums",
          fontSize: 12,
          cursor: "default",
          whiteSpace: "nowrap",
        }}
      >
        {n} {label}
      </span>
    </Tooltip>
  );
  return (
    <Space size={4}>
      {num(stats.done, "就绪", `${stats.done} 份副本已就绪`, token.colorText)}
      <span style={{ color: token.colorTextQuaternary }}>·</span>
      {num(stats.pending, "待补", `${stats.pending} 份副本待补齐,由补齐管道处理`, token.colorInfo)}
      <span style={{ color: token.colorTextQuaternary }}>·</span>
      {num(
        stats.failed,
        "失败",
        `${stats.failed} 份副本补齐失败,将由对账任务重试;持续失败请检查渠道配置`,
        token.colorError,
      )}
    </Space>
  );
}
