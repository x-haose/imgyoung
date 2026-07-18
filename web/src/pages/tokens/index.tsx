import { KeyOutlined, PlusOutlined } from "@ant-design/icons";
import { useInvalidate, useList } from "@refinedev/core";
import {
  App as AntdApp,
  Alert,
  Button,
  Card,
  Flex,
  Form,
  Input,
  Modal,
  Popconfirm,
  Table,
  Tag,
  Typography,
} from "antd";
import { useState } from "react";

import { apiFetch } from "../../api/http";
import { QueryState } from "../../components/QueryState";
import { formatTime } from "../../lib/format";

/** Token 列表项(对应 /api/admin/tokens 响应)。 */
interface TokenItem {
  id: number;
  name: string;
  prefix: string;
  scopes: string;
  last_used_at: string | null;
  revoked_at: string | null;
  created_at: string;
}

/** 签发成功响应:明文仅此一次。 */
interface IssuedToken {
  id: number;
  name: string;
  prefix: string;
  token: string;
}

/** Token 页:列表 + 签发(明文单次展示)+ 吊销。 */
export function TokensPage() {
  const [createOpen, setCreateOpen] = useState(false);
  const [issued, setIssued] = useState<IssuedToken | null>(null);
  const [creating, setCreating] = useState(false);
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm<{ name: string }>();

  const list = useList<TokenItem>({
    resource: "tokens",
    pagination: { currentPage: 1, pageSize: 100 },
  });
  const items = list.result?.data ?? [];

  const refresh = () => invalidate({ resource: "tokens", invalidates: ["list"] });

  const create = async (values: { name: string }) => {
    setCreating(true);
    try {
      const res = await apiFetch<IssuedToken>("/api/admin/tokens", {
        method: "POST",
        body: JSON.stringify(values),
      });
      setCreateOpen(false);
      form.resetFields();
      setIssued(res);
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "签发失败");
    } finally {
      setCreating(false);
    }
  };

  const revoke = async (id: number) => {
    try {
      await apiFetch(`/api/admin/tokens/${id}`, { method: "DELETE" });
      message.success("已吊销");
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "吊销失败");
    }
  };

  return (
    <Flex vertical gap={16}>
      <Flex align="center" justify="space-between">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            上传 Token
          </Typography.Title>
          <Typography.Text type="secondary">
            给 PicGo、脚本、CLI 各签发一个具名 Token;明文只在签发时展示一次
          </Typography.Text>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          签发 Token
        </Button>
      </Flex>

      <QueryState query={list.query}>
        <Card styles={{ body: { padding: 0 } }}>
          <Table<TokenItem>
            rowKey="id"
            dataSource={items}
            loading={list.query.isFetching}
            pagination={false}
            scroll={{ x: 760 }}
            size="middle"
            columns={[
              { title: "名称", dataIndex: "name" },
              {
                title: "识别前缀",
                dataIndex: "prefix",
                render: (v: string) => <Typography.Text code>{v}…</Typography.Text>,
              },
              {
                title: "状态",
                key: "state",
                render: (_, row) =>
                  row.revoked_at ? <Tag>已吊销</Tag> : <Tag color="success">有效</Tag>,
              },
              {
                title: "最近使用",
                dataIndex: "last_used_at",
                render: (v: string | null) => (v ? formatTime(v) : "从未使用"),
              },
              { title: "签发时间", dataIndex: "created_at", render: formatTime },
              {
                title: "操作",
                key: "actions",
                width: 100,
                render: (_, row) =>
                  row.revoked_at ? null : (
                    <Popconfirm
                      title="吊销该 Token?"
                      description="使用该 Token 的客户端将立即无法上传。"
                      okButtonProps={{ danger: true }}
                      onConfirm={() => revoke(row.id)}
                    >
                      <Button size="small" danger type="text">
                        吊销
                      </Button>
                    </Popconfirm>
                  ),
              },
            ]}
          />
        </Card>
      </QueryState>

      <Modal
        open={createOpen}
        title="签发上传 Token"
        onCancel={() => setCreateOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={creating}
        okText="签发"
      >
        <Form form={form} layout="vertical" onFinish={create}>
          <Form.Item
            name="name"
            label="用途名称"
            rules={[{ required: true, message: "请输入用途名称" }]}
            extra="如 picgo、blog-script;同名有效 Token 不可重复"
          >
            <Input maxLength={64} placeholder="picgo" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={issued !== null}
        title={
          <Flex align="center" gap={8}>
            <KeyOutlined /> Token 已签发
          </Flex>
        }
        // 明文只此一次:禁掉遮罩点击 / Esc / 右上角叉,唯一出口是二次确认过的「我已保存」。
        maskClosable={false}
        keyboard={false}
        closable={false}
        footer={
          <Popconfirm
            title="确认已保存?"
            description="关闭后无法再次查看明文。"
            okText="已保存,关闭"
            onConfirm={() => setIssued(null)}
          >
            <Button type="primary">我已保存</Button>
          </Popconfirm>
        }
      >
        <Alert
          type="warning"
          showIcon
          message="明文仅此一次展示,关闭后无法再次查看"
          style={{ marginBottom: 16 }}
        />
        <Typography.Paragraph
          code
          copyable
          style={{ wordBreak: "break-all", fontSize: 13, marginBottom: 8 }}
        >
          {issued?.token}
        </Typography.Paragraph>
        <Typography.Text type="secondary">
          用法:请求头 Authorization: Bearer &lt;token&gt;
        </Typography.Text>
      </Modal>
    </Flex>
  );
}
