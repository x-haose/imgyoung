import { FilterOutlined } from "@ant-design/icons";
import { useList } from "@refinedev/core";
import { Badge, Button, DatePicker, Flex, Popover, Select, Switch, Tag, Typography } from "antd";

import type { ImageFilterState } from "./types";

/** Token 下拉所需的最小切面(/api/admin/tokens 响应子集)。 */
interface TokenOption {
  id: number;
  name: string;
}

/** 用户下拉所需的最小切面(/api/admin/users 响应子集)。 */
interface UserOption {
  id: string;
  name: string;
}

/** 下拉选项一次拉取的上限;筛选器场景不做分页。 */
const optionPageSize = 100;

/** ImageFilters 的属性。 */
interface ImageFiltersProps {
  isAdmin: boolean; // 管理员额外提供归属用户筛选
  value: ImageFilterState; // 当前筛选状态(受控)
  onChange: (next: ImageFilterState) => void; // 任一维度变化时回调(调用方负责重置页码)
}

/** 图片筛选器:Popover 收纳 Token / 时间范围 /(管理员)用户三个维度,已激活项以可清除 Tag 平铺展示。 */
export function ImageFilters({ isAdmin, value, onChange }: ImageFiltersProps) {
  // Token 选项跟随用户维度:管理员选了用户则拉该用户的,否则拉本人的(后端同一语义)。
  const tokens = useList<TokenOption>({
    resource: "tokens",
    pagination: { currentPage: 1, pageSize: optionPageSize },
    filters: value.userId ? [{ field: "user_id", operator: "eq", value: value.userId }] : [],
  });
  const users = useList<UserOption>({
    resource: "users",
    pagination: { currentPage: 1, pageSize: optionPageSize },
    queryOptions: { enabled: isAdmin },
  });

  const tokenName = (id: number) =>
    tokens.result?.data.find((t) => t.id === id)?.name ?? `#${id}`;
  const activeCount =
    [value.tokenId, value.userId, value.range].filter((v) => v != null).length +
    (value.exemptOnly ? 1 : 0);

  const panel = (
    <Flex vertical gap={12} style={{ width: 280, paddingTop: 4 }}>
      {isAdmin && (
        <label>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            归属用户
          </Typography.Text>
          <Select
            allowClear
            placeholder="全部用户"
            style={{ width: "100%", marginTop: 4 }}
            value={value.userId ?? undefined}
            options={users.result?.data.map((u) => ({ value: u.id, label: u.name || u.id }))}
            // 换用户后 Token 归属随之变化,旧 Token 筛选一并清除。
            onChange={(v) => onChange({ ...value, userId: v ?? null, tokenId: null })}
          />
        </label>
      )}
      <label>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          上传来源 Token
        </Typography.Text>
        <Select
          allowClear
          placeholder="全部来源"
          style={{ width: "100%", marginTop: 4 }}
          value={value.tokenId ?? undefined}
          options={tokens.result?.data.map((t) => ({ value: t.id, label: t.name }))}
          onChange={(v) => onChange({ ...value, tokenId: v ?? null })}
        />
      </label>
      <label>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          上传时间
        </Typography.Text>
        <DatePicker.RangePicker
          allowClear
          style={{ width: "100%", marginTop: 4 }}
          value={value.range}
          onChange={(range) =>
            onChange({ ...value, range: range?.[0] && range[1] ? [range[0], range[1]] : null })
          }
        />
      </label>
      <Flex align="center" justify="space-between">
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          仅看已豁免(不参与冷图打标的图)
        </Typography.Text>
        <Switch
          size="small"
          checked={value.exemptOnly}
          onChange={(checked) => onChange({ ...value, exemptOnly: checked })}
        />
      </Flex>
    </Flex>
  );

  return (
    <>
      <Popover trigger="click" placement="bottomRight" content={panel}>
        <Badge count={activeCount} size="small">
          <Button icon={<FilterOutlined />}>筛选</Button>
        </Badge>
      </Popover>
      {value.userId && (
        <Tag closable onClose={() => onChange({ ...value, userId: null, tokenId: null })}>
          用户:{users.result?.data.find((u) => u.id === value.userId)?.name ?? value.userId}
        </Tag>
      )}
      {value.tokenId != null && (
        <Tag closable onClose={() => onChange({ ...value, tokenId: null })}>
          Token:{tokenName(value.tokenId)}
        </Tag>
      )}
      {value.range && (
        <Tag closable onClose={() => onChange({ ...value, range: null })}>
          {value.range[0].format("YYYY-MM-DD")} ~ {value.range[1].format("YYYY-MM-DD")}
        </Tag>
      )}
      {value.exemptOnly && (
        <Tag closable onClose={() => onChange({ ...value, exemptOnly: false })}>
          仅看已豁免
        </Tag>
      )}
    </>
  );
}
