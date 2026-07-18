import { CheckCircleFilled, LinkOutlined } from "@ant-design/icons";
import { App as AntdApp, Button, Card, Flex, Tag, Tooltip, Typography, theme } from "antd";
import { useState } from "react";

import { copyText } from "../../lib/clipboard";
import { absoluteURL, formatBytes } from "../../lib/format";
import type { ImageItem } from "./types";

/** ImageCard 的属性。 */
interface ImageCardProps {
  img: ImageItem; // 待展示的图片项
  selecting: boolean; // 是否处于多选模式(点卡片=勾选,不开抽屉)
  selected: boolean; // 多选模式下是否已勾选
  onOpen: (img: ImageItem) => void; // 常规模式点击:打开详情抽屉
  onToggle: (id: string) => void; // 多选模式点击:切换勾选
}

/** 图片卡片:缩略图 + 悬停一键复制;多选模式下整卡变为勾选目标。 */
export function ImageCard({ img, selecting, selected, onOpen, onToggle }: ImageCardProps) {
  const [hovered, setHovered] = useState(false);
  const { token } = theme.useToken();
  const { message } = AntdApp.useApp();

  /** copyURL 复制完整访问链接(不进抽屉,卡片上直接完成)。 */
  const copyURL = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (await copyText(absoluteURL(img.url))) {
      message.success("链接已复制");
    } else {
      message.error("复制失败,请手动复制");
    }
  };

  return (
    <Card
      hoverable
      onClick={() => (selecting ? onToggle(img.id) : onOpen(img))}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      styles={{ body: { padding: "10px 12px" } }}
      style={
        selecting && selected
          ? { outline: `2px solid ${token.colorPrimary}`, outlineOffset: -2 }
          : undefined
      }
      cover={
        <div
          style={{
            position: "relative",
            aspectRatio: "4 / 3",
            overflow: "hidden",
            background: token.colorBgLayout,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <img
            src={`${img.url}?w=400`}
            alt={img.file_name || img.id}
            loading="lazy"
            style={{ width: "100%", height: "100%", objectFit: "cover" }}
          />
          {selecting && (
            <CheckCircleFilled
              style={{
                position: "absolute",
                top: 8,
                left: 8,
                fontSize: 20,
                color: selected ? token.colorPrimary : "rgba(255,255,255,0.75)",
                filter: "drop-shadow(0 1px 2px rgba(0,0,0,0.4))",
              }}
            />
          )}
          {!selecting && (
            <div
              style={{
                position: "absolute",
                inset: "auto 0 0 0",
                padding: "20px 8px 8px",
                display: "flex",
                justifyContent: "flex-end",
                background: "linear-gradient(transparent, rgba(0,0,0,0.55))",
                opacity: hovered ? 1 : 0,
                transition: `opacity ${token.motionDurationMid}`,
                pointerEvents: hovered ? "auto" : "none",
              }}
            >
              <Tooltip title="复制链接">
                <Button
                  size="small"
                  type="primary"
                  ghost
                  icon={<LinkOutlined />}
                  style={{ borderColor: "rgba(255,255,255,0.85)", color: "#fff" }}
                  onClick={copyURL}
                >
                  复制
                </Button>
              </Tooltip>
            </div>
          )}
        </div>
      }
    >
      <Typography.Text ellipsis style={{ display: "block", fontSize: 12 }}>
        {img.file_name || img.id}
      </Typography.Text>
      <Flex justify="space-between" align="center" style={{ marginTop: 2 }}>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {formatBytes(img.size)}
        </Typography.Text>
        {img.pending_cleanup && <Tag color="warning">待清理</Tag>}
      </Flex>
    </Card>
  );
}
