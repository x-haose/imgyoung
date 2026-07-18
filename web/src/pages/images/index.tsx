import { BookOutlined, CheckSquareOutlined, DeleteOutlined, UploadOutlined } from "@ant-design/icons";
import { useInvalidate, useList, usePermissions } from "@refinedev/core";
import {
  App as AntdApp,
  Button,
  Col,
  Empty,
  Flex,
  Input,
  Pagination,
  Popconfirm,
  Row,
  Space,
  Typography,
  theme,
} from "antd";
import { useState } from "react";
import { Link } from "react-router";

import { apiFetch } from "../../api/http";
import { QueryState } from "../../components/QueryState";
import { usePageClamp } from "../../hooks/usePageClamp";
import { useServerConfig } from "../../hooks/useServerConfig";
import { deleteWarningText, toUTCText } from "../../lib/format";
import { ImageCard } from "./ImageCard";
import { ImageDetailDrawer } from "./ImageDetailDrawer";
import { ImageFilters } from "./ImageFilters";
import {
  batchDeleteMax,
  emptyFilters,
  type BatchDeleteResult,
  type ImageFilterState,
  type ImageItem,
} from "./types";
import { UploadModal } from "./UploadModal";

/** 每页卡片数(4 列 × 6 行)。 */
const pageSize = 24;

/** buildFilters 把搜索词与筛选状态平铺为后端列表查询参数。 */
function buildFilters(search: string, f: ImageFilterState) {
  const out: { field: string; operator: "eq"; value: string | number }[] = [];
  if (search) {
    out.push({ field: "search", operator: "eq", value: search });
  }
  if (f.tokenId != null) {
    out.push({ field: "token_id", operator: "eq", value: f.tokenId });
  }
  if (f.userId) {
    out.push({ field: "user_id", operator: "eq", value: f.userId });
  }
  if (f.range) {
    // 首尾两天按本地日界取闭开区间:[首日 00:00, 末日次日 00:00)。
    out.push({ field: "since", operator: "eq", value: toUTCText(f.range[0].startOf("day").valueOf()) });
    out.push({ field: "until", operator: "eq", value: toUTCText(f.range[1].add(1, "day").startOf("day").valueOf()) });
  }
  if (f.exemptOnly) {
    out.push({ field: "exempt", operator: "eq", value: 1 });
  }
  return out;
}

/** 图片页:缩略卡片网格 + 搜索/多维筛选 + 多选批量删除 + 详情抽屉。 */
export function ImagesPage() {
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<ImageFilterState>(emptyFilters);
  const [active, setActive] = useState<ImageItem | null>(null);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [batchDeleting, setBatchDeleting] = useState(false);
  const { data: role } = usePermissions<string>({});
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();
  const { token } = theme.useToken();

  const serverConfig = useServerConfig();
  const hasQuery =
    search !== "" ||
    filters.tokenId != null ||
    filters.userId != null ||
    filters.range != null ||
    filters.exemptOnly;
  const list = useList<ImageItem>({
    resource: "images",
    pagination: { currentPage: page, pageSize },
    filters: buildFilters(search, filters),
  });
  const items = list.result?.data ?? [];
  const total = list.result?.total ?? 0;
  usePageClamp(page, setPage, list.result?.total, pageSize);

  const refresh = () => invalidate({ resource: "images", invalidates: ["list"] });

  /** toggleSelect 切换单张勾选(不可变更新)。 */
  const toggleSelect = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else if (next.size >= batchDeleteMax) {
        message.warning(`一次最多选择 ${batchDeleteMax} 张`);
        return prev;
      } else {
        next.add(id);
      }
      return next;
    });
  };

  /** exitSelecting 退出多选模式并清空已选。 */
  const exitSelecting = () => {
    setSelecting(false);
    setSelected(new Set());
  };

  /** batchDelete 批量删除已选并反馈成功/失败数。 */
  const batchDelete = async () => {
    setBatchDeleting(true);
    try {
      const res = await apiFetch<BatchDeleteResult>("/api/admin/images/batch-delete", {
        method: "POST",
        body: JSON.stringify({ ids: [...selected] }),
      });
      if (res.failed.length > 0) {
        message.warning(`已删除 ${res.deleted} 张,${res.failed.length} 张失败:${res.failed[0].error}`);
      } else {
        message.success(`已删除 ${res.deleted} 张`);
      }
      exitSelecting();
      setActive(null);
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "批量删除失败");
    } finally {
      setBatchDeleting(false);
    }
  };

  return (
    <Flex vertical gap={16}>
      <Flex align="center" justify="space-between" wrap gap={12}>
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            图片
          </Typography.Title>
          <Typography.Text type="secondary">
            {role === "admin" ? "全站图片" : "我的图片"} · 共 {total} 张
          </Typography.Text>
        </div>
        <Space wrap>
          <Input.Search
            allowClear
            placeholder="按短 ID / 文件名搜索"
            style={{ width: 240 }}
            onSearch={(v) => {
              setPage(1);
              setSearch(v.trim());
            }}
          />
          <ImageFilters
            isAdmin={role === "admin"}
            value={filters}
            onChange={(next) => {
              setPage(1);
              setFilters(next);
            }}
          />
          <Button
            icon={<CheckSquareOutlined />}
            type={selecting ? "primary" : "default"}
            ghost={selecting}
            onClick={() => (selecting ? exitSelecting() : setSelecting(true))}
          >
            {selecting ? "取消选择" : "选择"}
          </Button>
          <Button type="primary" icon={<UploadOutlined />} onClick={() => setUploadOpen(true)}>
            上传图片
          </Button>
        </Space>
      </Flex>

      <QueryState query={list.query}>
        {items.length === 0 ? (
          hasQuery ? (
            <Empty description="没有匹配的图片" style={{ padding: "64px 0" }} />
          ) : (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              style={{ padding: "64px 0" }}
              description={
                <Flex vertical gap={4}>
                  <Typography.Text strong>还没有图片</Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                    从这里直接上传,或用 PicGo / curl 等客户端带 Token 上传
                  </Typography.Text>
                </Flex>
              }
            >
              <Space wrap>
                <Button type="primary" icon={<UploadOutlined />} onClick={() => setUploadOpen(true)}>
                  上传第一张图
                </Button>
                <Link to="/guide">
                  <Button icon={<BookOutlined />}>查看使用指南</Button>
                </Link>
              </Space>
            </Empty>
          )
        ) : (
          <Row gutter={[16, 16]}>
            {items.map((img) => (
              <Col key={img.id} xs={12} sm={8} md={6} xl={4}>
                <ImageCard
                  img={img}
                  selecting={selecting}
                  selected={selected.has(img.id)}
                  onOpen={setActive}
                  onToggle={toggleSelect}
                />
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

      {selecting && (
        <Flex
          align="center"
          gap={16}
          style={{
            position: "fixed",
            bottom: 24,
            left: "50%",
            transform: "translateX(-50%)",
            zIndex: 10,
            padding: "10px 20px",
            borderRadius: 12,
            background: token.colorBgElevated,
            boxShadow: token.boxShadowSecondary,
            border: `1px solid ${token.colorBorderSecondary}`,
          }}
        >
          <Typography.Text strong>已选 {selected.size} 张</Typography.Text>
          <Popconfirm
            title={`删除选中的 ${selected.size} 张图片?`}
            description={deleteWarningText(serverConfig?.delete_grace_minutes)}
            okButtonProps={{ danger: true }}
            onConfirm={() => void batchDelete()}
          >
            <Button danger icon={<DeleteOutlined />} loading={batchDeleting} disabled={selected.size === 0}>
              批量删除
            </Button>
          </Popconfirm>
          <Button onClick={exitSelecting}>取消</Button>
        </Flex>
      )}

      <ImageDetailDrawer
        image={active}
        onClose={() => setActive(null)}
        onDeleted={() => {
          setActive(null);
          refresh();
        }}
        onPatched={(updated) => {
          setActive(updated);
          refresh();
        }}
      />

      <UploadModal open={uploadOpen} onClose={() => setUploadOpen(false)} onUploaded={refresh} />
    </Flex>
  );
}
