import { Button, Result } from "antd";
import { Link } from "react-router";

/** 404 兜底页:未知路径不再白屏,给出回总览的出路。 */
export function NotFoundPage() {
  return (
    <Result
      status="404"
      title="页面不存在"
      subTitle="地址可能输错了,或该页面已被移除"
      extra={
        <Link to="/">
          <Button type="primary">返回总览</Button>
        </Link>
      }
    />
  );
}
