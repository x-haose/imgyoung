import { BookOutlined, CloudServerOutlined, KeyOutlined, PlusOutlined, SyncOutlined } from "@ant-design/icons";
import { useInvalidate, useList, usePermissions } from "@refinedev/core";
import {
  App as AntdApp,
  Alert,
  Button,
  Card,
  Empty,
  Flex,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import { useState } from "react";
import { Link } from "react-router";

import { apiFetch } from "../../api/http";
import { QueryState } from "../../components/QueryState";
import { usePageClamp } from "../../hooks/usePageClamp";
import { formatTime } from "../../lib/format";
import { sessionUser } from "../../providers/authProvider";
import { S3CredentialModal, type S3Credential } from "./S3CredentialModal";

/** Token 列表项(对应 /api/admin/tokens 响应)。 */
interface TokenItem {
  id: number;
  name: string;
  prefix: string;
  scopes: string;
  expires_at: string | null;
  last_used_at: string | null;
  revoked_at: string | null;
  created_at: string;
}

/** 用户下拉项(管理员筛选用,对应 /api/admin/users 响应的子集)。 */
interface UserOption {
  id: string;
  name: string;
}

/** 签发/轮转成功响应:明文仅此一次。 */
interface IssuedToken {
  id: number;
  name: string;
  prefix: string;
  token: string;
}

/** 签发表单值。 */
interface CreateForm {
  name: string;
  expires_in_hours: number;
}

/** 有效期档位;value 为小时数,0 表示永久。 */
const expiryOptions = [
  { value: 0, label: "永久" },
  { value: 7 * 24, label: "7 天" },
  { value: 30 * 24, label: "30 天" },
  { value: 90 * 24, label: "90 天" },
  { value: 365 * 24, label: "365 天" },
];

/** 每页 Token 数(后端单次上限 100)。 */
const pageSize = 50;

/** Token 的展示状态:有效 / 已过期 / 已吊销。 */
type TokenStatus = "active" | "expired" | "revoked";

/** 判定 Token 当前状态;后端时间为 UTC 文本,按 UTC 解析与现在比较。 */
function tokenStatus(row: TokenItem): TokenStatus {
  if (row.revoked_at) {
    return "revoked";
  }
  if (row.expires_at && new Date(row.expires_at.replace(" ", "T") + "Z").getTime() <= Date.now()) {
    return "expired";
  }
  return "active";
}

/** 状态 → 三色 Tag。 */
const statusTag: Record<TokenStatus, { color: string; label: string }> = {
  active: { color: "success", label: "有效" },
  expired: { color: "warning", label: "已过期" },
  revoked: { color: "default", label: "已吊销" },
};

/** Token 页:列表(分页/三态)+ 签发(可选有效期)+ 轮转 + 吊销 + 失效记录删除。 */
export function TokensPage() {
  const [page, setPage] = useState(1);
  const [createOpen, setCreateOpen] = useState(false);
  const [issued, setIssued] = useState<IssuedToken | null>(null);
  const [s3Credential, setS3Credential] = useState<S3Credential | null>(null);
  const [s3TokenName, setS3TokenName] = useState("");
  const [creating, setCreating] = useState(false);
  const [userFilter, setUserFilter] = useState<string | undefined>(undefined);
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm<CreateForm>();
  const { data: role } = usePermissions<string>({});

  const isAdmin = role === "admin";
  const me = sessionUser();
  // 未选择或选中本人 = 本人视角;查看他人时隐藏签发与轮转(明文只应下发给持有者)。
  const viewingSelf = !userFilter || userFilter === me?.id;

  const list = useList<TokenItem>({
    resource: "tokens",
    pagination: { currentPage: page, pageSize },
    filters: userFilter ? [{ field: "user_id", operator: "eq", value: userFilter }] : [],
  });
  const items = list.result?.data ?? [];
  const total = list.result?.total ?? 0;
  usePageClamp(page, setPage, list.result?.total, pageSize);

  const users = useList<UserOption>({
    resource: "users",
    pagination: { currentPage: 1, pageSize: 100 },
    queryOptions: { enabled: isAdmin },
  });

  const refresh = () => invalidate({ resource: "tokens", invalidates: ["list"] });

  const create = async (values: CreateForm) => {
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

  const rotate = async (id: number) => {
    try {
      const res = await apiFetch<IssuedToken>(`/api/admin/tokens/${id}/rotate`, { method: "POST" });
      setIssued(res);
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "轮转失败");
    }
  };

  const showS3Credential = async (row: TokenItem) => {
    try {
      const res = await apiFetch<S3Credential>(`/api/admin/tokens/${row.id}/s3-credential`, {
        method: "POST",
      });
      setS3TokenName(row.name);
      setS3Credential(res);
    } catch (err) {
      message.error(err instanceof Error ? err.message : "获取 S3 凭证失败");
    }
  };

  const revoke = async (id: number) => {
    try {
      await apiFetch(`/api/admin/tokens/${id}/revoke`, { method: "POST" });
      message.success("已吊销");
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "吊销失败");
    }
  };

  const removeRecord = async (id: number) => {
    try {
      await apiFetch(`/api/admin/tokens/${id}`, { method: "DELETE" });
      message.success("记录已删除");
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
            上传 Token
          </Typography.Title>
          <Typography.Text type="secondary">
            给 PicGo、脚本、CLI 各签发一个具名 Token;明文只在签发时展示一次
          </Typography.Text>
        </div>
        <Space>
          {isAdmin && (
            <Select
              style={{ width: 180 }}
              value={userFilter ?? me?.id}
              onChange={(v) => {
                setPage(1);
                setUserFilter(v === me?.id ? undefined : v);
              }}
              options={(users.result?.data ?? []).map((u) => ({
                value: u.id,
                label: u.id === me?.id ? `${u.name}(本人)` : u.name,
              }))}
              loading={users.query.isFetching}
              placeholder="按用户查看"
            />
          )}
          {viewingSelf && (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
              签发 Token
            </Button>
          )}
        </Space>
      </Flex>

      <QueryState query={list.query}>
        <Card styles={{ body: { padding: 0 } }}>
          <Table<TokenItem>
            rowKey="id"
            dataSource={items}
            loading={list.query.isFetching}
            pagination={{
              current: page,
              pageSize,
              total,
              showSizeChanger: false,
              onChange: setPage,
              hideOnSinglePage: true,
            }}
            scroll={{ x: 920 }}
            size="middle"
            locale={{
              emptyText: (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  style={{ padding: "40px 0" }}
                  description={
                    <Flex vertical gap={4}>
                      <Typography.Text strong>还没有 Token</Typography.Text>
                      <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                        给 PicGo 或脚本签发第一个,客户端才能调上传接口
                      </Typography.Text>
                    </Flex>
                  }
                >
                  <Space wrap>
                    {viewingSelf && (
                      <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
                        签发第一个 Token
                      </Button>
                    )}
                    <Link to="/guide">
                      <Button icon={<BookOutlined />}>查看使用指南</Button>
                    </Link>
                  </Space>
                </Empty>
              ),
            }}
            columns={[
              {
                title: "名称",
                dataIndex: "name",
              },
              {
                title: "识别前缀",
                dataIndex: "prefix",
                render: (v: string) => <Typography.Text code>{v}…</Typography.Text>,
              },
              {
                title: "状态",
                key: "state",
                render: (_, row) => {
                  const s = statusTag[tokenStatus(row)];
                  return <Tag color={s.color}>{s.label}</Tag>;
                },
              },
              {
                title: "到期",
                dataIndex: "expires_at",
                render: (v: string | null) => (v ? formatTime(v) : "永久"),
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
                width: 220,
                render: (_, row) => {
                  if (tokenStatus(row) === "active") {
                    return (
                      <Space size={0}>
                        {viewingSelf && (
                          <Button
                            size="small"
                            type="text"
                            icon={<CloudServerOutlined />}
                            onClick={() => showS3Credential(row)}
                          >
                            S3 配置
                          </Button>
                        )}
                        {viewingSelf && (
                          <Popconfirm
                            title="轮转该 Token?"
                            description="生成同名新 Token,旧 Token 立即失效;新明文仅展示一次。"
                            onConfirm={() => rotate(row.id)}
                          >
                            <Button size="small" type="text" icon={<SyncOutlined />}>
                              轮转
                            </Button>
                          </Popconfirm>
                        )}
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
                      </Space>
                    );
                  }
                  return (
                    <Popconfirm
                      title="删除该记录?"
                      description="失效记录从列表移除,审计留痕保留。"
                      okButtonProps={{ danger: true }}
                      onConfirm={() => removeRecord(row.id)}
                    >
                      <Button size="small" danger type="text">
                        删除
                      </Button>
                    </Popconfirm>
                  );
                },
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
        <Form
          form={form}
          layout="vertical"
          onFinish={create}
          initialValues={{ expires_in_hours: 0 }}
        >
          <Form.Item
            name="name"
            label="用途名称"
            rules={[{ required: true, message: "请输入用途名称" }]}
            extra="如 picgo、blog-script;同名有效 Token 不可重复"
          >
            <Input maxLength={64} placeholder="picgo" />
          </Form.Item>
          <Form.Item
            name="expires_in_hours"
            label="有效期"
            extra="到期后 Token 自动失效,与吊销同等对待"
          >
            <Select options={expiryOptions} />
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

      <S3CredentialModal
        credential={s3Credential}
        tokenName={s3TokenName}
        onClose={() => setS3Credential(null)}
      />
    </Flex>
  );
}
