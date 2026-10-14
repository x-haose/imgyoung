import { Card, Flex, Typography, theme } from "antd";
import { useState } from "react";

import type { ChannelTemplate } from "./types";

/** 自定义模式的哨兵模板 id;后端按此值走 driver+config 直填路径。 */
export const CUSTOM = "custom";

/** 选类型网格的分组;顺序即展示顺序,key 与后端 Template.category 对齐,自定义单列一组收在末尾。 */
const CATEGORY_GROUPS: { key: string; label: string }[] = [
  { key: "object_storage", label: "对象存储" },
  { key: "drive", label: "网盘" },
  { key: CUSTOM, label: "高级" },
];

/** 自定义模式在网格中的占位模板;不来自后端,仅供渲染。 */
const CUSTOM_TEMPLATE = {
  id: CUSTOM,
  name: "自定义",
  description: "手动填 rclone 参数",
} as ChannelTemplate;

/** 各存储类型的辨识徽标:短标 + 品牌色;纯 CSS 方块渲染,不引外部图(守 CSP 与体积预算)。 */
const TYPE_BADGES: Record<string, { text: string; color: string }> = {
  "aws-s3": { text: "S3", color: "#FF9900" },
  r2: { text: "R2", color: "#5B6EF5" },
  cos: { text: "COS", color: "#00A4FF" },
  oss: { text: "OSS", color: "#FF6A00" },
  b2: { text: "B2", color: "#E21E29" },
  minio: { text: "M", color: "#C7264B" },
  "s3-compatible": { text: "S3", color: "#8C8C8C" },
  webdav: { text: "DAV", color: "#0082C9" },
  onedrive: { text: "OD", color: "#0078D4" },
  googledrive: { text: "GD", color: "#1A73E8" },
  dropbox: { text: "DB", color: "#0061FF" },
  pcloud: { text: "PC", color: "#00A3E0" },
  box: { text: "BOX", color: "#0061D5" },
  yandex: { text: "YA", color: "#FC3F1D" },
  [CUSTOM]: { text: "⚙", color: "#8C8C8C" },
};

/** 兜底徽标:未登记的模板 id 用中性灰 S3 标。 */
const FALLBACK_BADGE = { text: "S3", color: "#8C8C8C" };

/** TypeBadge 存储类型的品牌徽标方块;size 控制边长,三字符短标自动缩字号保持居中。 */
export function TypeBadge({ id, size }: { id: string; size: number }) {
  const badge = TYPE_BADGES[id] ?? FALLBACK_BADGE;
  const dense = badge.text.length >= 3;
  return (
    <div
      style={{
        width: size,
        height: size,
        flex: "0 0 auto",
        borderRadius: size * 0.28,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: badge.color,
        background: `${badge.color}1F`,
        fontWeight: 700,
        fontSize: dense ? size * 0.32 : size * 0.44,
        letterSpacing: dense ? 0 : 0.5,
      }}
    >
      {badge.text}
    </div>
  );
}

/** TemplateCard 单个存储类型卡片;hover 时描边转主色,过渡只走 border-color。 */
function TemplateCard({
  tpl,
  hovered,
  onHover,
  onSelect,
}: {
  tpl: ChannelTemplate;
  hovered: boolean;
  onHover: (id: string) => void;
  onSelect: (id: string) => void;
}) {
  const { token } = theme.useToken();
  return (
    <Card
      size="small"
      hoverable
      onClick={() => onSelect(tpl.id)}
      onMouseEnter={() => onHover(tpl.id)}
      onMouseLeave={() => onHover("")}
      styles={{ body: { padding: 12 } }}
      style={{
        cursor: "pointer",
        transition: "border-color .2s",
        borderColor: hovered ? token.colorPrimary : token.colorBorderSecondary,
      }}
    >
      <Flex gap={10} align="center">
        <TypeBadge id={tpl.id} size={36} />
        <div style={{ minWidth: 0 }}>
          <Typography.Text strong style={{ display: "block", fontSize: 13 }}>
            {tpl.name}
          </Typography.Text>
          <Typography.Text
            type="secondary"
            ellipsis={{ tooltip: tpl.description }}
            style={{ fontSize: 11, display: "block" }}
          >
            {tpl.description}
          </Typography.Text>
        </div>
      </Flex>
    </Card>
  );
}

/**
 * TemplatePicker 渠道创建的选类型步:按分类分组的品牌徽标网格。
 * 同屏分组而非两级下钻——选项一屏放得下时,多收一次点击是净损失。
 */
export function TemplatePicker({
  templates,
  onSelect,
}: {
  templates: ChannelTemplate[]; // 后端下发的模板,自定义项由本组件补齐
  onSelect: (id: string) => void; // 点选后切到配置步
}) {
  const { token } = theme.useToken();
  const [hoverId, setHoverId] = useState(""); // hover 高亮项;仅驱动描边过渡

  return (
    <div style={{ paddingTop: 4 }}>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        选择存储类型
      </Typography.Text>
      {CATEGORY_GROUPS.map(({ key, label }) => {
        const group = key === CUSTOM ? [CUSTOM_TEMPLATE] : templates.filter((t) => t.category === key);
        if (group.length === 0) {
          return null;
        }
        return (
          <div key={key}>
            <Typography.Text
              strong
              style={{
                display: "block",
                fontSize: 11,
                letterSpacing: 0.6,
                textTransform: "uppercase",
                color: token.colorTextTertiary,
                marginTop: 18,
              }}
            >
              {label}
            </Typography.Text>
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
                gap: 10,
                marginTop: 8,
              }}
            >
              {group.map((tpl) => (
                <TemplateCard
                  key={tpl.id}
                  tpl={tpl}
                  hovered={hoverId === tpl.id}
                  onHover={setHoverId}
                  onSelect={onSelect}
                />
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
}
