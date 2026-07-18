import { Flex, Typography, theme } from "antd";
import { useState, type ReactNode } from "react";
import { Link } from "react-router";

/** QuickStepCard 的属性。 */
interface QuickStepCardProps {
  icon: ReactNode; // 步骤图标
  title: string; // 步骤名(调用方自带序号)
  desc: ReactNode; // 步骤说明(可含页面链接;卡片自身可点时应为纯文本,避免嵌套锚点)
  to?: string; // 目标路由;提供时整卡是链接,hover / 聚焦有主色描边反馈
}

/**
 * 快速开始步骤卡:边框 + 浅底 + 图标的统一观感,总览页与指南页共用同一渲染,消除文案与样式漂移。
 * 传入 to 时整卡可点,悬停 / 键盘聚焦以主色描边与加深底色示意入口。
 */
export function QuickStepCard({ icon, title, desc, to }: QuickStepCardProps) {
  const { token } = theme.useToken();
  const [active, setActive] = useState(false);
  const highlighted = to !== undefined && active;
  const card = (
    <Flex
      vertical
      gap={8}
      style={{
        height: "100%",
        padding: "16px 16px 14px",
        border: `1px solid ${highlighted ? token.colorPrimaryBorderHover : token.colorBorderSecondary}`,
        borderRadius: 10,
        background: highlighted ? token.colorFillTertiary : token.colorFillQuaternary,
        transition: `border-color ${token.motionDurationMid}, background ${token.motionDurationMid}`,
      }}
    >
      <Flex align="center" gap={10}>
        {icon}
        <Typography.Text strong>{title}</Typography.Text>
      </Flex>
      <Typography.Text type="secondary" style={{ fontSize: 13, lineHeight: 1.7 }}>
        {desc}
      </Typography.Text>
    </Flex>
  );
  if (to === undefined) {
    return card;
  }
  return (
    <Link
      to={to}
      style={{ display: "block", height: "100%", borderRadius: 10 }}
      onMouseEnter={() => setActive(true)}
      onMouseLeave={() => setActive(false)}
      onFocus={() => setActive(true)}
      onBlur={() => setActive(false)}
    >
      {card}
    </Link>
  );
}
