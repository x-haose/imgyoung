import {
  BookOutlined,
  CloseOutlined,
  CloudUploadOutlined,
  ClusterOutlined,
  KeyOutlined,
  RocketOutlined,
} from "@ant-design/icons";
import { useCustom } from "@refinedev/core";
import { Button, Card, Col, Flex, Row, Statistic, Table, Tag, Tooltip, Typography, theme } from "antd";
import { useState } from "react";
import { Link } from "react-router";

import { QueryState } from "../../components/QueryState";
import { QuickStepCard } from "../../components/QuickStepCard";
import { useServerConfig } from "../../hooks/useServerConfig";
import { adminThumbSrc, formatBytes } from "../../lib/format";

/** 仪表盘聚合响应(管理员视图;普通用户仅前三项有意义)。 */
interface DashboardData {
  images: number;
  storage_bytes?: number;
  used_bytes?: number;
  quota_bytes?: number | null;
  users?: number;
  pending_cleanup?: number;
  pending_dedup?: number;
  total_accesses?: number;
  traffic_estimate_bytes?: number;
  top_images?: TopImage[];
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

/** 热门图项(按访问次数降序,最多 10 条)。 */
interface TopImage {
  id: string;
  url: string;
  file_name: string;
  access_count: number;
  size: number;
}

/** 总览页:核心指标卡片 + 渠道健康表;待裁决计数与异常状态可点击直达处置页。 */
export function DashboardPage() {
  const { query } = useCustom<DashboardData>({ url: "/api/admin/dashboard", method: "get" });
  const data = query.data?.data;
  const isAdmin = data?.users !== undefined;
  const topImages = data?.top_images ?? [];
  const [quickStartClosed, setQuickStartClosed] = useState(false);
  const { token } = theme.useToken();
  const serverConfig = useServerConfig();

  /** 快速开始的三步导引;仅在还没有任何图片时展示,渠道页仅管理员可进。 */
  const quickStartSteps = [
    {
      to: "/channels",
      icon: <ClusterOutlined style={{ fontSize: 22, color: token.colorPrimary }} />,
      label: "确认存储渠道",
      hint: "已内置 local 本地渠道,可直接用",
      adminOnly: true,
    },
    {
      to: "/tokens",
      icon: <KeyOutlined style={{ fontSize: 22, color: token.colorPrimary }} />,
      label: "签发上传 Token",
      hint: "给 PicGo、脚本各一个具名 Token",
      adminOnly: false,
    },
    {
      to: "/images",
      icon: <CloudUploadOutlined style={{ fontSize: 22, color: token.colorPrimary }} />,
      label: "上传第一张图",
      hint: "后台可直接拖拽 / 粘贴,无需 Token",
      adminOnly: false,
    },
  ];

  return (
    <Flex vertical gap={16}>
      <Typography.Title level={4} style={{ margin: 0 }}>
        总览
      </Typography.Title>
      <QueryState query={query}>
        {data?.images === 0 && !quickStartClosed && (
          <Card
            title={
              <Flex align="center" gap={8}>
                <RocketOutlined />
                快速开始
              </Flex>
            }
            extra={
              <Button
                type="text"
                size="small"
                icon={<CloseOutlined />}
                aria-label="关闭快速开始"
                onClick={() => setQuickStartClosed(true)}
              />
            }
          >
            <Flex vertical gap={12}>
              <Row gutter={[16, 16]}>
                {quickStartSteps
                  .filter((s) => isAdmin || !s.adminOnly)
                  .map((s, i) => (
                    <Col key={s.to} xs={24} md={8}>
                      <QuickStepCard
                        icon={s.icon}
                        title={`${i + 1}. ${s.label}`}
                        desc={s.hint}
                        to={s.to}
                      />
                    </Col>
                  ))}
              </Row>
              <Link to="/guide">
                <Button size="small" icon={<BookOutlined />}>
                  查看完整使用指南
                </Button>
              </Link>
            </Flex>
          </Card>
        )}
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
              <Col xs={12} md={6}>
                <Card>
                  <Statistic
                    title={
                      <Tooltip title="按活跃度落库周期计数,同一图片同周期内多次访问计 1 次,非精确 PV">
                        <span>访问次数</span>
                      </Tooltip>
                    }
                    value={data?.total_accesses ?? 0}
                  />
                </Card>
              </Col>
              <Col xs={12} md={6}>
                <Card>
                  <Statistic
                    title={
                      <Tooltip title="按访问次数 × 原图大小的粗略估算,不代表真实出站流量(304/302/缩略变体也按整图计)">
                        <span>估算流量</span>
                      </Tooltip>
                    }
                    value={formatBytes(data?.traffic_estimate_bytes ?? 0)}
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
        {isAdmin && topImages.length > 0 && (
          <Card title="热门图片 Top 10">
            <Flex vertical gap={8}>
              {topImages.map((img, i) => (
                <a
                  key={img.id}
                  href={img.url}
                  target="_blank"
                  rel="noreferrer"
                  title="打开原图外链"
                  style={{ color: "inherit" }}
                >
                  <Flex align="center" gap={12}>
                    <Typography.Text type="secondary" style={{ width: 20, textAlign: "right", fontSize: 12 }}>
                      {i + 1}
                    </Typography.Text>
                    <img
                      src={adminThumbSrc(img.id, serverConfig?.process_widths, 100)}
                      alt={img.file_name || img.id}
                      loading="lazy"
                      width={48}
                      height={48}
                      style={{ objectFit: "cover", borderRadius: 6, background: token.colorBgLayout }}
                    />
                    <Flex vertical style={{ flex: 1, minWidth: 0 }}>
                      <Typography.Text ellipsis>{img.file_name || img.id}</Typography.Text>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        {formatBytes(img.size)}
                      </Typography.Text>
                    </Flex>
                    <Typography.Text strong>{img.access_count} 次</Typography.Text>
                  </Flex>
                </a>
              ))}
            </Flex>
          </Card>
        )}
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
