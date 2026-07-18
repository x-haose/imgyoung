import { useCustom } from "@refinedev/core";
import { Card, Col, Flex, Row, Statistic, Table, Tag, Typography } from "antd";
import { Link } from "react-router";

import { QueryState } from "../../components/QueryState";
import { formatBytes } from "../../lib/format";

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

/** 总览页:核心指标卡片 + 渠道健康表;待裁决计数与异常状态可点击直达处置页。 */
export function DashboardPage() {
  const { query } = useCustom<DashboardData>({ url: "/api/admin/dashboard", method: "get" });
  const data = query.data?.data;
  const isAdmin = data?.users !== undefined;

  return (
    <Flex vertical gap={16}>
      <Typography.Title level={4} style={{ margin: 0 }}>
        总览
      </Typography.Title>
      <QueryState query={query}>
        <Row gutter={[16, 16]}>
          <Col xs={12} md={6}>
            <Card>
              <Statistic title="图片" value={data?.images ?? 0} />
            </Card>
          </Col>
          <Col xs={12} md={6}>
            <Card>
              <Statistic
                title="存储用量"
                value={formatBytes(isAdmin ? data?.storage_bytes : data?.used_bytes)}
              />
            </Card>
          </Col>
          {isAdmin ? (
            <>
              <Col xs={12} md={6}>
                <Card>
                  <Statistic title="用户" value={data?.users ?? 0} />
                </Card>
              </Col>
              <Col xs={12} md={6}>
                <Card>
                  <Statistic
                    title="待裁决(清理/重复)"
                    valueRender={() => (
                      <span>
                        <Link to="/cleanup" title="去待清理页裁决">
                          {data?.pending_cleanup ?? 0}
                        </Link>
                        {" / "}
                        <Link to="/dedup" title="去疑似重复页裁决">
                          {data?.pending_dedup ?? 0}
                        </Link>
                      </span>
                    )}
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
              pagination={false}
              size="middle"
              scroll={{ x: 520 }}
              columns={[
                { title: "渠道", dataIndex: "name" },
                { title: "驱动", dataIndex: "driver" },
                {
                  title: "状态",
                  dataIndex: "healthy",
                  render: (healthy: boolean) =>
                    healthy ? (
                      <Tag color="success">健康</Tag>
                    ) : (
                      <Link to="/channels" title="去渠道页查看">
                        <Tag color="error" style={{ cursor: "pointer" }}>
                          异常
                        </Tag>
                      </Link>
                    ),
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
      </QueryState>
    </Flex>
  );
}
