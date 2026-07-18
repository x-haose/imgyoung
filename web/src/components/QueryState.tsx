import { ReloadOutlined } from "@ant-design/icons";
import { Button, Card, Result, Skeleton } from "antd";
import type { ReactNode } from "react";

/** QueryState 所需的最小查询状态切面(useList().query / useCustom().query 均满足)。 */
interface QueryLike {
  isLoading: boolean; // 首次加载且尚无数据
  isError: boolean; // 请求失败
  error?: unknown; // 失败原因,展示 Error.message
  refetch: () => unknown; // 重试入口
}

/** QueryState 的属性。 */
interface QueryStateProps {
  query: QueryLike; // 查询状态来源
  children: ReactNode; // 数据就绪后的正常内容
}

/** errorMessage 提取用户可读的失败原因;非 Error 对象回落通用文案。 */
function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return "未知错误";
}

/**
 * QueryState 统一列表/统计页的三态渲染:
 * 失败 → 带具体原因与重试按钮的错误占位(避免失败被渲染成"空态/全 0");
 * 首次加载 → 骨架屏;其余 → children。
 */
export function QueryState({ query, children }: QueryStateProps) {
  if (query.isError) {
    return (
      <Card>
        <Result
          status="error"
          title="加载失败"
          subTitle={errorMessage(query.error)}
          extra={
            <Button type="primary" icon={<ReloadOutlined />} onClick={() => query.refetch()}>
              重试
            </Button>
          }
        />
      </Card>
    );
  }
  if (query.isLoading) {
    return (
      <Card>
        <Skeleton active paragraph={{ rows: 6 }} />
      </Card>
    );
  }
  return children;
}
