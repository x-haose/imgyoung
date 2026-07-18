import { useList } from "@refinedev/core";
import { Card, DatePicker, Flex, Input, Select, Table, Tag, Tooltip, Typography } from "antd";
import type { Dayjs } from "dayjs";
import { useState } from "react";
import { useSearchParams } from "react-router";

import { QueryState } from "../../components/QueryState";
import { usePageClamp } from "../../hooks/usePageClamp";
import { formatTime, toUTCText } from "../../lib/format";

/** 审计记录项(对应 /api/admin/audit 响应)。 */
interface AuditItem {
  id: number;
  actor: string;
  actor_name: string;
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
  if (action.startsWith("referer.")) {
    return "green";
  }
  return "default";
}

/** 可筛选的动作清单(与后端 audit 常量一致)。 */
const actionOptions = [
  "channel.create",
  "channel.update",
  "channel.delete",
  "token.issue",
  "token.revoke",
  "token.rotate",
  "token.delete",
  "user.create",
  "user.update",
  "user.delete",
  "image.update",
  "image.delete",
  "data.export",
  "dedup.merge",
  "dedup.dismiss",
  "cleanup.keep",
  "referer.add",
  "referer.remove",
].map((v) => ({ value: v, label: v }));

/** 每页审计条数。 */
const pageSize = 50;

/** 审计页:管理操作留痕,按操作者/动作/目标/时间范围过滤;目标过滤可由其他页带 query 直达。 */
export function AuditPage() {
  const [searchParams] = useSearchParams();
  const [page, setPage] = useState(1);
  const [actor, setActor] = useState("");
  const [action, setAction] = useState("");
  const [target, setTarget] = useState(() => searchParams.get("target") ?? "");
  const [range, setRange] = useState<[Dayjs, Dayjs] | null>(null);

  const list = useList<AuditItem>({
    resource: "audit",
    pagination: { currentPage: page, pageSize },
    filters: [
      ...(actor ? [{ field: "actor", operator: "eq" as const, value: actor }] : []),
      ...(action ? [{ field: "action", operator: "eq" as const, value: action }] : []),
      ...(target ? [{ field: "target", operator: "eq" as const, value: target }] : []),
      ...(range
        ? [
            // 首尾两天按本地日界取闭开区间:[首日 00:00, 末日次日 00:00)。
            {
              field: "since",
              operator: "eq" as const,
              value: toUTCText(range[0].startOf("day").valueOf()),
            },
            {
              field: "until",
              operator: "eq" as const,
              value: toUTCText(range[1].add(1, "day").startOf("day").valueOf()),
            },
          ]
        : []),
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
        <Flex gap={8} wrap>
          <Select
            allowClear
            placeholder="按动作过滤"
            style={{ width: 180 }}
            options={actionOptions}
            onChange={(v) => {
              setPage(1);
              setAction(v ?? "");
            }}
          />
          <Input.Search
            allowClear
            placeholder="按操作者 id 过滤"
            style={{ width: 170 }}
            onSearch={(v) => {
              setPage(1);
              setActor(v.trim());
            }}
          />
          <Input.Search
            allowClear
            placeholder="目标 ID(图片短 ID / 渠道 ID / Token ID)"
            style={{ width: 260 }}
            defaultValue={target}
            onSearch={(v) => {
              setPage(1);
              setTarget(v.trim());
            }}
          />
          <DatePicker.RangePicker
            allowClear
            value={range}
            onChange={(next) => {
              setPage(1);
              setRange(next?.[0] && next[1] ? [next[0], next[1]] : null);
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
              {
                title: "操作者",
                dataIndex: "actor_name",
                width: 160,
                render: (name: string, row) => (
                  <Tooltip title={`用户 id:${row.actor}`}>
                    <span>{name}</span>
                  </Tooltip>
                ),
              },
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
