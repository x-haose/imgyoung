import { useCustom } from "@refinedev/core";
import { Card, Col, Flex, Row, Statistic, Table, Tag, Typography } from "antd";

/** 仪表盘聚合响应(管理员视图;普通用户仅前三项有意义)。 */
interface DashboardData {
  images: number;
  storage_bytes?: number;
  used_bytes?: number;
  quota_bytes?: number | null;
  users?: number;
  pending_cleanup?: number;
  pending_dedup?: number;
  channels?: ChannelStat[];
}

/** 渠道概览项。 */
interface ChannelStat {
  id: number;
  name: string;
  driver: string;
  healthy: boolean;
  bytes: number;
}

/** 把字节格式化为人类可读单位。 */
export function formatBytes(n: number | undefined | null): string {
  if (n === undefined || n === null) {
    return "—";
  }
  if (n < 1024) {
    return `${n} B`;
  }
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`;
}

/** 总览页:核心指标卡片 + 渠道健康表。 */
export function DashboardPage() {
  const { query } = useCustom<DashboardData>({ url: "/api/admin/dashboard", method: "get" });
  const data = query.data?.data;
  const isAdmin = data?.users !== undefined;

  return (
    <Flex vertical gap={16}>
      <Typography.Title level={4} style={{ margin: 0 }}>
        总览
      </Typography.Title>
      <Row gutter={[16, 16]}>
        <Col xs={12} md={6}>
          <Card>
            <Statistic title="图片" value={data?.images ?? 0} loading={query.isLoading} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card>
            <Statistic
              title="存储用量"
              value={formatBytes(isAdmin ? data?.storage_bytes : data?.used_bytes)}
              loading={query.isLoading}
            />
          </Card>
        </Col>
        {isAdmin ? (
          <>
            <Col xs={12} md={6}>
              <Card>
                <Statistic title="用户" value={data?.users ?? 0} loading={query.isLoading} />
              </Card>
            </Col>
            <Col xs={12} md={6}>
              <Card>
                <Statistic
                  title="待裁决(清理/重复)"
                  value={`${data?.pending_cleanup ?? 0} / ${data?.pending_dedup ?? 0}`}
                  loading={query.isLoading}
                />
              </Card>
            </Col>
          </>
        ) : (
          <Col xs={12} md={6}>
            <Card>
              <Statistic
                title="配额"
                value={data?.quota_bytes == null ? "不限" : formatBytes(data.quota_bytes)}
                loading={query.isLoading}
              />
            </Card>
          </Col>
        )}
      </Row>
      {isAdmin && (
        <Card title="存储渠道" styles={{ body: { padding: 0 } }}>
          <Table<ChannelStat>
            rowKey="id"
            dataSource={data?.channels ?? []}
            loading={query.isLoading}
            pagination={false}
            size="middle"
            columns={[
              { title: "渠道", dataIndex: "name" },
              { title: "驱动", dataIndex: "driver" },
              {
                title: "状态",
                dataIndex: "healthy",
                render: (healthy: boolean) =>
                  healthy ? <Tag color="success">健康</Tag> : <Tag color="error">异常</Tag>,
              },
              {
                title: "已存",
                dataIndex: "bytes",
                align: "right",
                render: (v: number) => formatBytes(v),
              },
            ]}
          />
        </Card>
      )}
    </Flex>
  );
}
