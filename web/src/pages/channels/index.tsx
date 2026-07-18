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
  Typography,
} from "antd";
import { useState } from "react";

import { apiFetch } from "../../api/http";
import { QueryState } from "../../components/QueryState";
import { CreateChannelModal } from "./CreateChannelModal";

/** 渠道列表项(channel.Info,凭证与配置不出库)。 */
interface ChannelItem {
  id: number;
  name: string;
  driver: string;
  priority: number;
  healthy: boolean;
  encrypted: boolean;
  allow_direct_link: boolean;
}

/** 渠道页:列表 + 模板化创建 + 删除(唯一副本守卫由后端兜底)。 */
export function ChannelsPage() {
  const [open, setOpen] = useState(false);
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();

  const list = useList<ChannelItem>({
    resource: "channels",
    pagination: { currentPage: 1, pageSize: 100 },
  });
  const items = list.result?.data ?? [];
  const refresh = () => invalidate({ resource: "channels", invalidates: ["list"] });

  const remove = async (id: number) => {
    try {
      await apiFetch(`/api/admin/channels/${id}`, { method: "DELETE" });
      message.success("渠道已删除");
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "删除失败");
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
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
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
            scroll={{ x: 640 }}
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
                  <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
                    新建渠道
                  </Button>
                </Empty>
              ),
            }}
            columns={[
              { title: "渠道", dataIndex: "name" },
              { title: "驱动", dataIndex: "driver", render: (v: string) => <Tag>{v}</Tag> },
              { title: "优先级", dataIndex: "priority", align: "right", width: 90 },
              {
                title: "状态",
                dataIndex: "healthy",
                width: 90,
                render: (v: boolean) =>
                  v ? <Tag color="success">健康</Tag> : <Tag color="error">异常</Tag>,
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
                width: 100,
                render: (_, row) => (
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
                ),
              },
            ]}
          />
        </Card>
      </QueryState>

      <CreateChannelModal open={open} onClose={() => setOpen(false)} onCreated={() => {
        setOpen(false);
        refresh();
      }} />
    </Flex>
  );
}
