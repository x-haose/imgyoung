import { PlusOutlined } from "@ant-design/icons";
import { useInvalidate, useList } from "@refinedev/core";
import {
  App as AntdApp,
  Button,
  Card,
  Checkbox,
  Empty,
  Flex,
  Modal,
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

/** 渠道页:列表(健康详情 + 副本水位)+ 模板化创建/编辑 + 手动重检 + 删除(可选清理远端对象)。 */
export function ChannelsPage() {
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<ChannelItem | null>(null);
  const [checkingId, setCheckingId] = useState<number | null>(null);
  const [removing, setRemoving] = useState<ChannelItem | null>(null); // 删除确认框里的目标渠道
  const [purge, setPurge] = useState(true); // 删除确认框里的「同时清理远端对象」
  const [removingBusy, setRemovingBusy] = useState(false);
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

  // 删除两条路径:清理远端对象走异步排空,渠道先停留为「清理中」;不清理则立即移除。
  const remove = async (id: number, purgeObjects: boolean) => {
    setRemovingBusy(true);
    try {
      await apiFetch(`/api/admin/channels/${id}?purge=${purgeObjects}`, { method: "DELETE" });
      message.success(purgeObjects ? "渠道已开始清理,对象删完后自动移除" : "渠道已移除,远端对象保留");
      setRemoving(null);
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "删除失败");
    } finally {
      setRemovingBusy(false);
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
                render: (_, row) =>
                  row.deleting ? (
                    // 清理中的渠道只剩一条出路:放弃清理、直接移除(远端对象保留)。
                    <Popconfirm
                      title={`直接移除渠道 ${row.name}?`}
                      description="停止清理,立即移除渠道;尚未删除的远端对象原样保留。"
                      okButtonProps={{ danger: true }}
                      onConfirm={() => remove(row.id, false)}
                    >
                      <Button size="small" danger type="text">
                        直接移除
                      </Button>
                    </Popconfirm>
                  ) : (
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
                      <Button
                        size="small"
                        danger
                        type="text"
                        onClick={() => {
                          setPurge(true);
                          setRemoving(row);
                        }}
                      >
                        删除
                      </Button>
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

      <Modal
        open={removing !== null}
        title={`删除渠道 ${removing?.name ?? ""}`}
        okText={purge ? "删除并清理" : "仅移除"}
        okButtonProps={{ danger: true }}
        confirmLoading={removingBusy}
        onOk={() => removing && void remove(removing.id, purge)}
        onCancel={() => setRemoving(null)}
      >
        <Flex vertical gap={12}>
          <Typography.Text type="secondary">
            持有唯一副本的渠道会被拒绝删除,防止丢数据。
          </Typography.Text>
          <Checkbox checked={purge} onChange={(e) => setPurge(e.target.checked)}>
            同时删除该渠道上已存储的对象
          </Checkbox>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {purge
              ? "对象在后台逐个删除,期间渠道显示为「清理中」,删完自动移除;凭证已失效或存储桶已不存在时请取消勾选。"
              : "渠道立即移除,远端对象原样保留,需要时可自行到存储商处理。"}
          </Typography.Text>
        </Flex>
      </Modal>
    </Flex>
  );
}

/** HealthCell 状态列:健康/异常 Tag(异常带失败原因),下方小字显示最近探测的相对时间。 */
function HealthCell({ row }: { row: ChannelItem }) {
  return (
    <Flex vertical gap={2} align="flex-start">
      {row.deleting ? (
        <Tooltip title={row.last_error || "正在删除渠道上的对象,删完自动移除"}>
          <Tag color="warning" style={{ cursor: "help" }}>
            清理中
          </Tag>
        </Tooltip>
      ) : row.healthy ? (
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
