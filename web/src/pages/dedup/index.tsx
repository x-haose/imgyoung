import { useInvalidate, useList } from "@refinedev/core";
import {
  App as AntdApp,
  Button,
  Card,
  Col,
  Empty,
  Flex,
  Pagination,
  Row,
  Tag,
  Tooltip,
  Typography,
  theme,
} from "antd";
import { useState } from "react";

import { apiFetch } from "../../api/http";
import { QueryState } from "../../components/QueryState";
import { usePageClamp } from "../../hooks/usePageClamp";
import { formatBytes } from "../../lib/format";

/** 候选对中一方的展示信息。 */
interface CandidateSide {
  blob_id: number;
  image_id: string;
  mime: string;
  size: number;
  width: number;
  height: number;
  refs: number;
}

/** 相似候选项(对应 /api/admin/dedup 响应)。 */
interface CandidateItem {
  id: number;
  distance: number;
  created_at: string;
  a: CandidateSide;
  b: CandidateSide;
}

/** 单侧预览卡:图 + 元数据 + 保留按钮。 */
function SideCard({
  side,
  label,
  onKeep,
}: {
  side: CandidateSide;
  label: string;
  onKeep: () => void;
}) {
  const { token } = theme.useToken();
  return (
    <Flex vertical gap={8} style={{ flex: 1, minWidth: 0 }}>
      <div
        style={{
          aspectRatio: "4 / 3",
          overflow: "hidden",
          borderRadius: 8,
          background: token.colorBgLayout,
        }}
      >
        {side.image_id ? (
          <a href={`/i/${side.image_id}`} target="_blank" rel="noreferrer">
            <img
              src={`/i/${side.image_id}?w=400`}
              alt={label}
              loading="lazy"
              style={{ width: "100%", height: "100%", objectFit: "contain" }}
            />
          </a>
        ) : (
          <Flex align="center" justify="center" style={{ height: "100%" }}>
            <Typography.Text type="secondary">无引用可预览</Typography.Text>
          </Flex>
        )}
      </div>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {side.mime} · {side.width}×{side.height} · {formatBytes(side.size)} · {side.refs} 个引用
      </Typography.Text>
      <Button onClick={onKeep} block>
        保留{label}
      </Button>
    </Flex>
  );
}

/** 每页候选组数。 */
const pageSize = 20;

/** 疑似重复页:候选并排对比,人工裁决合并方向或忽略。 */
export function DedupPage() {
  const [page, setPage] = useState(1);
  const invalidate = useInvalidate();
  const { message, modal } = AntdApp.useApp();

  const list = useList<CandidateItem>({
    resource: "dedup",
    pagination: { currentPage: page, pageSize },
  });
  const items = list.result?.data ?? [];
  const total = list.result?.total ?? 0;
  usePageClamp(page, setPage, list.result?.total, pageSize);
  const refresh = () => invalidate({ resource: "dedup", invalidates: ["list"] });

  const merge = (cand: CandidateItem, keep: CandidateSide, drop: CandidateSide) => {
    modal.confirm({
      title: "确认合并?",
      content: `被并一侧的 ${drop.refs} 个引用将改指向保留图,原物理文件按宽限期清理;此操作不可撤销。`,
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await apiFetch(`/api/admin/dedup/${cand.id}/merge`, {
            method: "POST",
            body: JSON.stringify({ keep_blob_id: keep.blob_id }),
          });
          message.success("已合并");
          refresh();
        } catch (err) {
          message.error(err instanceof Error ? err.message : "合并失败");
          refresh();
        }
      },
    });
  };

  const dismiss = async (cand: CandidateItem) => {
    try {
      await apiFetch(`/api/admin/dedup/${cand.id}/dismiss`, { method: "POST" });
      message.success("已忽略,该组不再提示");
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "忽略失败");
    }
  };

  return (
    <Flex vertical gap={16}>
      <div>
        <Typography.Title level={4} style={{ margin: 0 }}>
          疑似重复
        </Typography.Title>
        <Typography.Text type="secondary">
          pHash 相似的图片对,共 {total} 组待裁决;机器发现,人做不可逆决定
        </Typography.Text>
      </div>

      <QueryState query={list.query}>
        {items.length === 0 ? (
          <Empty description="没有待裁决的相似图片" style={{ padding: "64px 0" }} />
        ) : (
          <Row gutter={[16, 16]}>
            {items.map((cand) => (
              <Col key={cand.id} xs={24} xl={12}>
                <Card
                  title={
                    <Flex align="center" gap={8}>
                      <Tooltip title="感知哈希差异位数,越小越相似;0–2 几乎相同,越大差异越大">
                        <Tag color={cand.distance <= 2 ? "error" : "warning"}>
                          距离 {cand.distance}
                        </Tag>
                      </Tooltip>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        #{cand.id}
                      </Typography.Text>
                    </Flex>
                  }
                  extra={
                    <Button size="small" type="text" onClick={() => dismiss(cand)}>
                      忽略
                    </Button>
                  }
                >
                  <Flex gap={16}>
                    <SideCard side={cand.a} label="左图" onKeep={() => merge(cand, cand.a, cand.b)} />
                    <SideCard side={cand.b} label="右图" onKeep={() => merge(cand, cand.b, cand.a)} />
                  </Flex>
                </Card>
              </Col>
            ))}
          </Row>
        )}
        <Flex justify="center">
          <Pagination
            current={page}
            pageSize={pageSize}
            total={total}
            showSizeChanger={false}
            onChange={setPage}
            hideOnSinglePage
          />
        </Flex>
      </QueryState>
    </Flex>
  );
}
