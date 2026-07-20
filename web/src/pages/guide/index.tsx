import {
  ApiOutlined,
  CameraOutlined,
  CloudUploadOutlined,
  ClusterOutlined,
  KeyOutlined,
  PictureOutlined,
} from "@ant-design/icons";
import { Alert, Card, Col, Descriptions, Flex, Row, Table, Tag, Typography, theme } from "antd";
import type { ReactNode } from "react";
import { Link } from "react-router";

import { QuickStepCard } from "../../components/QuickStepCard";

/** 代码块的等宽字体栈。 */
const monoFont =
  "'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace";

/** 代码块配色:两种主题下都用深色底,像终端一样可辨识、可复制。 */
const codeColors = {
  bg: "#0d1117",
  border: "#1f272e",
  text: "#dbe4ec",
} as const;

/** CodeBlock 的属性。 */
interface CodeBlockProps {
  code: string; // 展示与复制的完整文本
}

/** 深色底代码块,右上角一键复制。 */
function CodeBlock({ code }: CodeBlockProps) {
  return (
    <div
      style={{
        position: "relative",
        background: codeColors.bg,
        border: `1px solid ${codeColors.border}`,
        borderRadius: 8,
        padding: "12px 40px 12px 16px",
      }}
    >
      <Typography.Text
        copyable={{ text: code, tooltips: ["复制", "已复制"] }}
        style={{ position: "absolute", top: 10, right: 12, color: "#7d8590" }}
      />
      <pre
        style={{
          margin: 0,
          overflowX: "auto",
          fontFamily: monoFont,
          fontSize: 12.5,
          lineHeight: 1.7,
          color: codeColors.text,
        }}
      >
        {code}
      </pre>
    </div>
  );
}

/** 快速开始的单步卡片描述。 */
interface QuickStep {
  icon: ReactNode; // 步骤图标
  title: string; // 步骤名
  desc: ReactNode; // 步骤说明(可含页面链接)
}

/** 上传接口的错误码速查表行。 */
interface ErrorRow {
  code: number; // HTTP 状态码
  meaning: string; // 触发原因
  advice: string; // 处理建议
}

/** 错误码表数据;与服务端上传错误映射一一对应。 */
const errorRows: ErrorRow[] = [
  { code: 400, meaning: "请求有误:file 字段缺失、图片内容无法解析、ttl / exif_policy 取值非法", advice: "检查表单字段与取值格式" },
  { code: 401, meaning: "Token 无效、已吊销或已过期", advice: "在 Token 页重新签发或轮转" },
  { code: 403, meaning: "Token 无上传权限,或存储配额已满", advice: "确认 Token 权限;配额满时删图或联系管理员扩容" },
  { code: 413, meaning: "文件超过单图大小上限", advice: "压缩后重试,或调大 max_upload_mb 配置" },
  { code: 415, meaning: "不支持的文件类型", advice: "仅支持 jpeg / png / gif / webp,按内容识别、不认扩展名" },
  { code: 422, meaning: "图片分辨率超出像素预算", advice: "缩小分辨率后重试" },
  { code: 429, meaning: "上传过于频繁或并发已满", advice: "按响应头 Retry-After 稍后重试" },
  { code: 503, meaning: "存储渠道暂不可用", advice: "稍后重试;持续出现时到渠道页检查健康状态" },
];

/** 状态码 → Tag 颜色:4xx 客户端问题用橙,5xx 服务端问题用红。 */
function codeColor(code: number): string {
  return code >= 500 ? "error" : "warning";
}

/** 带序号徽标的小节卡片标题。 */
function sectionTitle(no: string, icon: ReactNode, text: string) {
  return (
    <Flex align="center" gap={10}>
      <Typography.Text type="secondary" style={{ fontFamily: monoFont, fontSize: 12 }}>
        {no}
      </Typography.Text>
      {icon}
      <span>{text}</span>
    </Flex>
  );
}

/** 使用指南页:纯静态,面向刚装好的站长,覆盖三步上手 / curl / PicGo / 错误码。 */
export function GuidePage() {
  const { token } = theme.useToken();
  const origin = window.location.origin;

  const quickSteps: QuickStep[] = [
    {
      icon: <ClusterOutlined style={{ fontSize: 22, color: token.colorPrimary }} />,
      title: "确认存储渠道",
      desc: (
        <>
          已内置 local 本地渠道,开箱即用;要接 S3 / R2 等云存储,
          到<Link to="/channels">存储渠道</Link>页新建(仅管理员)。
        </>
      ),
    },
    {
      icon: <KeyOutlined style={{ fontSize: 22, color: token.colorPrimary }} />,
      title: "签发上传 Token",
      desc: (
        <>
          到<Link to="/tokens">上传 Token</Link>页给 PicGo、脚本各签发一个具名 Token,
          明文只展示一次;后台直接上传不需要 Token。
        </>
      ),
    },
    {
      icon: <CloudUploadOutlined style={{ fontSize: 22, color: token.colorPrimary }} />,
      title: "开始上传",
      desc: (
        <>
          用 curl / PicGo 带 Token 调上传接口,或直接在
          <Link to="/images">图片</Link>页拖拽、粘贴上传。
        </>
      ),
    },
  ];

  const curlExample = [
    `curl -H "Authorization: Bearer <你的 Token>" \\`,
    `  -F "file=@photo.png" \\`,
    `  ${origin}/api/upload`,
  ].join("\n");

  const responseExample = [
    `{`,
    `  "id": "R0RjUyP1UW",`,
    `  "url": "${origin}/i/R0RjUyP1UW"`,
    `}`,
  ].join("\n");

  return (
    <Flex vertical gap={16}>
      <div>
        <Typography.Title level={4} style={{ margin: 0 }}>
          使用指南
        </Typography.Title>
        <Typography.Text type="secondary">
          三步接入你的博客、笔记与截图工作流;本页地址均按当前站点生成,可直接复制
        </Typography.Text>
      </div>

      <Card title={sectionTitle("01", <CloudUploadOutlined />, "快速开始")}>
        <Row gutter={[16, 16]}>
          {quickSteps.map((s, i) => (
            <Col key={s.title} xs={24} md={8}>
              <QuickStepCard icon={s.icon} title={`${i + 1}. ${s.title}`} desc={s.desc} />
            </Col>
          ))}
        </Row>
      </Card>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card title={sectionTitle("02", <ApiOutlined />, "curl / 脚本上传")} style={{ height: "100%" }}>
            <Flex vertical gap={12}>
              <CodeBlock code={curlExample} />
              <Descriptions
                column={1}
                size="small"
                bordered
                items={[
                  { label: "file", children: "必填,multipart 文件字段;按内容识别格式,仅收 jpeg / png / gif / webp" },
                  { label: "ttl", children: "可选,过期秒数;0 或 never 表示永久,留空跟随默认(用户默认有效期,未设置则全局默认)" },
                  { label: "exif_policy", children: "可选,strip(剥离元数据)/ passthrough(原样保留),留空跟随全局默认" },
                  { label: "Idempotency-Key 头", children: "可选,幂等键;网络重试时带同一键不会重复建图" },
                ]}
              />
              <div>
                <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                  成功响应(200):
                </Typography.Text>
                <div style={{ marginTop: 8 }}>
                  <CodeBlock code={responseExample} />
                </div>
              </div>
            </Flex>
          </Card>
        </Col>

        <Col xs={24} xl={12}>
          <Card title={sectionTitle("03", <CameraOutlined />, "PicGo / PicList 配置")} style={{ height: "100%" }}>
            <Flex vertical gap={12}>
              <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                使用 PicList 自带的「自定义图床」或 PicGo 的 web-uploader 插件,按下表逐项填写:
              </Typography.Text>
              <Descriptions
                column={1}
                size="small"
                bordered
                items={[
                  {
                    label: "API 地址",
                    children: (
                      <Typography.Text code copyable style={{ fontFamily: monoFont }}>
                        {origin}/api/upload
                      </Typography.Text>
                    ),
                  },
                  { label: "请求方式", children: "POST" },
                  { label: "POST 参数名", children: <Typography.Text code>file</Typography.Text> },
                  {
                    label: "自定义请求头",
                    children: (
                      <Typography.Text code copyable style={{ fontFamily: monoFont, fontSize: 12 }}>
                        {'{"Authorization": "Bearer 你的Token"}'}
                      </Typography.Text>
                    ),
                  },
                  { label: "响应 URL 路径(JSON 路径)", children: <Typography.Text code>url</Typography.Text> },
                ]}
              />
              <Alert
                type="info"
                showIcon
                message="未配置 base_url 时,返回的 url 按请求域名推导为绝对地址,直连场景开箱即用;经反代 / CDN 后对外域名与请求域名不一致时,需在服务端配置 base_url(环境变量 IMGYOUNG_BASE_URL)为你的对外域名。"
              />
            </Flex>
          </Card>
        </Col>
      </Row>

      <Card
        title={sectionTitle("04", <ApiOutlined />, "上传接口错误码")}
        styles={{ body: { padding: 0 } }}
      >
        <Table<ErrorRow>
          rowKey="code"
          dataSource={errorRows}
          pagination={false}
          size="middle"
          scroll={{ x: 640 }}
          columns={[
            {
              title: "状态码",
              dataIndex: "code",
              width: 100,
              render: (v: number) => <Tag color={codeColor(v)}>{v}</Tag>,
            },
            { title: "含义", dataIndex: "meaning" },
            { title: "处理建议", dataIndex: "advice", width: 280 },
          ]}
        />
      </Card>

      <Card title={sectionTitle("05", <PictureOutlined />, "后台直接上传")}>
        <Typography.Text type="secondary" style={{ fontSize: 13, lineHeight: 1.8 }}>
          不想配客户端?在<Link to="/images">图片</Link>页点「上传图片」,
          支持拖拽、点选与 Ctrl / ⌘ + V 粘贴截图,走当前登录身份、无需 Token。
          上传后每张图都有短链,可一键复制。
        </Typography.Text>
      </Card>
    </Flex>
  );
}
