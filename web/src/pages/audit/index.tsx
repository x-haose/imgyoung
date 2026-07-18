import { useList } from "@refinedev/core";
import { Card, Flex, Input, Select, Table, Tag, Typography } from "antd";
import { useState } from "react";

import { QueryState } from "../../components/QueryState";
import { usePageClamp } from "../../hooks/usePageClamp";
import { formatTime } from "../../lib/format";

/** 审计记录项(对应 /api/admin/audit 响应)。 */
interface AuditItem {
  id: number;
  actor: string;
  action: string;
  target: string;
  detail: string;
  created_at: string;
}

/** 动作 → 标签色:按领域着色,便于扫读。 */
function actionColor(action: string): string {
  if (action.startsWith("channel.")) {
    return "geekblue";
  }
  if (action.startsWith("user.")) {
    return "gold";
  }
  if (action.startsWith("token.")) {
    return "cyan";
  }
  if (action.startsWith("dedup.") || action.startsWith("cleanup.")) {
    return "purple";
  }
  if (action.startsWith("image.")) {
    return "volcano";
  }
  return "default";
}

/** 可筛选的动作清单(与后端 audit 常量一致)。 */
const actionOptions = [
  "channel.create",
  "channel.delete",
  "token.issue",
  "token.revoke",
  "token.rotate",
  "token.delete",
  "user.create",
  "user.update",
  "user.delete",
  "image.delete",
  "data.export",
  "dedup.merge",
  "dedup.dismiss",
  "cleanup.keep",
].map((v) => ({ value: v, label: v }));

/** 每页审计条数。 */
const pageSize = 50;

/** 审计页:管理操作留痕,按操作者/动作过滤。 */
export function AuditPage() {
  const [page, setPage] = useState(1);
  const [actor, setActor] = useState("");
  const [action, setAction] = useState("");

  const list = useList<AuditItem>({
    resource: "audit",
    pagination: { currentPage: page, pageSize },
    filters: [
      ...(actor ? [{ field: "actor", operator: "eq" as const, value: actor }] : []),
      ...(action ? [{ field: "action", operator: "eq" as const, value: action }] : []),
    ],
  });
  const items = list.result?.data ?? [];
  const total = list.result?.total ?? 0;
  usePageClamp(page, setPage, list.result?.total, pageSize);

  return (
    <Flex vertical gap={16}>
      <Flex align="center" justify="space-between" wrap gap={12}>
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            审计日志
          </Typography.Title>
          <Typography.Text type="secondary">管理操作只追加留痕,共 {total} 条</Typography.Text>
        </div>
        <Flex gap={8}>
          <Select
            allowClear
            placeholder="按动作过滤"
            style={{ width: 200 }}
            options={actionOptions}
            onChange={(v) => {
              setPage(1);
              setAction(v ?? "");
            }}
          />
          <Input.Search
            allowClear
            placeholder="按操作者 id 过滤"
            style={{ width: 200 }}
            onSearch={(v) => {
              setPage(1);
              setActor(v.trim());
            }}
          />
        </Flex>
      </Flex>

      <QueryState query={list.query}>
        <Card styles={{ body: { padding: 0 } }}>
          <Table<AuditItem>
            rowKey="id"
            dataSource={items}
            loading={list.query.isFetching}
            size="middle"
            pagination={{
              current: page,
              pageSize,
              total,
              showSizeChanger: false,
              onChange: setPage,
            }}
            scroll={{ x: 900 }}
            columns={[
              { title: "时间", dataIndex: "created_at", width: 180, render: formatTime },
              {
                title: "动作",
                dataIndex: "action",
                width: 160,
                render: (v: string) => <Tag color={actionColor(v)}>{v}</Tag>,
              },
              { title: "操作者", dataIndex: "actor", width: 160 },
              { title: "目标", dataIndex: "target" },
              {
                title: "详情",
                dataIndex: "detail",
                render: (v: string) => (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {v}
                  </Typography.Text>
                ),
              },
            ]}
          />
        </Card>
      </QueryState>
    </Flex>
  );
}
