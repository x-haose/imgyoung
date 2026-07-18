import {
  CheckCircleFilled,
  CloseCircleFilled,
  InboxOutlined,
  LoadingOutlined,
} from "@ant-design/icons";
import { Flex, InputNumber, Modal, Typography, Upload, theme } from "antd";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import { apiFetch } from "../../api/http";
import { absoluteURL } from "../../lib/format";

/** 单个上传条目的展示状态。 */
interface UploadEntry {
  uid: number; // 本地自增标识
  name: string; // 文件名(剪贴板图片可能为空,用占位名)
  status: "uploading" | "done" | "error"; // 上传中 / 成功 / 失败
  url?: string; // 成功后的访问路径;配置了 base_url 时为绝对地址,否则为相对路径
  error?: string; // 失败时的后端错误消息
}

/** UploadModal 的属性。 */
interface UploadModalProps {
  open: boolean; // 是否可见
  onClose: () => void; // 请求关闭(关闭不打断进行中的上传)
  onUploaded: () => void; // 队列中全部文件完成且至少一张成功时回调(用于刷新列表)
}

/** 同时进行的上传数上限;解码在服务端,前端只需避免一次性塞爆并发闸。 */
const uploadConcurrency = 2;

/** 与后端格式白名单一致的 accept 提示(仅影响文件选择器,真校验在服务端)。 */
const acceptTypes = "image/jpeg,image/png,image/gif,image/webp";

/** 上传成功响应(与 /api/admin/upload 契约一致)。 */
interface UploadResult {
  id: string;
  url: string;
}

/**
 * 上传弹窗:拖拽 / 点选 / 粘贴多图,受控并发逐个走后台会话上传端点。
 * 关闭弹窗不打断进行中的上传;全部落定后经 onUploaded 通知父级刷新。
 */
export function UploadModal({ open, onClose, onUploaded }: UploadModalProps) {
  const [entries, setEntries] = useState<UploadEntry[]>([]);
  const [ttlHours, setTtlHours] = useState<number | null>(null);
  const { token } = theme.useToken();

  // 队列状态全放 ref:上传回调跨渲染周期存活,不能依赖闭包里的旧 state。
  const nextUid = useRef(0);
  const running = useRef(0);
  const pending = useRef<{ uid: number; file: File }[]>([]);
  const anySucceeded = useRef(false);
  const ttlRef = useRef<number | null>(null);
  ttlRef.current = ttlHours;

  /** patch 以不可变方式更新指定条目的状态。 */
  const patch = useCallback((uid: number, changes: Partial<UploadEntry>) => {
    setEntries((prev) => prev.map((e) => (e.uid === uid ? { ...e, ...changes } : e)));
  }, []);

  /** send 上传单个文件并落定其条目状态;TTL 取发起时刻的输入值(小时 → 秒)。 */
  const send = useCallback(
    async (uid: number, file: File) => {
      const fd = new FormData();
      fd.append("file", file, file.name || "clipboard.png");
      const hours = ttlRef.current;
      if (hours != null && hours > 0) {
        fd.append("ttl", String(Math.round(hours * 3600)));
      }
      try {
        const res = await apiFetch<UploadResult>("/api/admin/upload", {
          method: "POST",
          body: fd,
        });
        anySucceeded.current = true;
        patch(uid, { status: "done", url: res.url });
      } catch (err) {
        patch(uid, { status: "error", error: err instanceof Error ? err.message : "上传失败" });
      }
    },
    [patch],
  );

  /** pump 从待传队列取任务直到并发额度用满;队列清空且有成功时通知父级。 */
  const pump = useCallback(() => {
    while (running.current < uploadConcurrency && pending.current.length > 0) {
      const job = pending.current.shift();
      if (!job) {
        return;
      }
      running.current += 1;
      void send(job.uid, job.file).finally(() => {
        running.current -= 1;
        if (running.current === 0 && pending.current.length === 0 && anySucceeded.current) {
          anySucceeded.current = false;
          onUploaded();
        }
        pump();
      });
    }
  }, [send, onUploaded]);

  /** enqueue 把文件加入展示列表与待传队列并触发调度。 */
  const enqueue = useCallback(
    (file: File) => {
      const uid = nextUid.current;
      nextUid.current += 1;
      setEntries((prev) => [
        ...prev,
        { uid, name: file.name || "剪贴板图片", status: "uploading" },
      ]);
      pending.current.push({ uid, file });
      pump();
    },
    [pump],
  );

  // 弹窗打开期间监听全局 paste:剪贴板里的图片直接入队。
  useEffect(() => {
    if (!open) {
      return;
    }
    const onPaste = (e: ClipboardEvent) => {
      const files = Array.from(e.clipboardData?.items ?? [])
        .filter((it) => it.kind === "file" && it.type.startsWith("image/"))
        .map((it) => it.getAsFile())
        .filter((f): f is File => f !== null);
      if (files.length > 0) {
        e.preventDefault();
        files.forEach(enqueue);
      }
    };
    document.addEventListener("paste", onPaste);
    return () => document.removeEventListener("paste", onPaste);
  }, [open, enqueue]);

  // 重新打开且无进行中任务时清空上一轮记录;有任务在跑则保留,让用户看到结果。
  useEffect(() => {
    if (open && running.current === 0 && pending.current.length === 0) {
      setEntries([]);
    }
  }, [open]);

  const statusIcon: Record<UploadEntry["status"], ReactNode> = {
    uploading: <LoadingOutlined spin style={{ color: token.colorPrimary, fontSize: 16 }} />,
    done: <CheckCircleFilled style={{ color: token.colorSuccess, fontSize: 16 }} />,
    error: <CloseCircleFilled style={{ color: token.colorError, fontSize: 16 }} />,
  };

  return (
    <Modal open={open} onCancel={onClose} footer={null} title="上传图片" width={560} maskClosable={false}>
      <Flex vertical gap={16} style={{ paddingTop: 8 }}>
        <Upload.Dragger
          multiple
          accept={acceptTypes}
          showUploadList={false}
          customRequest={({ file }) => enqueue(file as File)}
        >
          <InboxOutlined style={{ fontSize: 40, color: token.colorPrimary }} />
          <Typography.Paragraph strong style={{ margin: "12px 0 4px" }}>
            点击或拖拽图片到此处
          </Typography.Paragraph>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            支持多选与 Ctrl / ⌘ + V 粘贴剪贴板图片;jpeg · png · gif · webp
          </Typography.Text>
        </Upload.Dragger>

        <Flex align="center" gap={10} wrap>
          <Typography.Text style={{ fontSize: 13 }}>有效期</Typography.Text>
          <InputNumber
            min={1}
            precision={0}
            placeholder="默认"
            suffix="小时"
            style={{ width: 130 }}
            value={ttlHours}
            onChange={(v) => setTtlHours(v)}
          />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            留空跟随全局默认;对之后加入的文件生效
          </Typography.Text>
        </Flex>

        {entries.length > 0 && (
          <Flex vertical gap={8} style={{ maxHeight: 300, overflowY: "auto" }}>
            {entries.map((e) => (
              <Flex
                key={e.uid}
                align="center"
                justify="space-between"
                gap={12}
                style={{
                  padding: "8px 12px",
                  border: `1px solid ${token.colorBorderSecondary}`,
                  borderRadius: 8,
                  background: token.colorFillQuaternary,
                }}
              >
                <Flex vertical style={{ minWidth: 0, flex: 1 }}>
                  <Typography.Text ellipsis style={{ fontSize: 13 }}>
                    {e.name}
                  </Typography.Text>
                  {e.status === "done" && e.url && (
                    <Typography.Text
                      type="secondary"
                      copyable={{
                        text: absoluteURL(e.url),
                        tooltips: ["复制完整链接", "已复制"],
                      }}
                      style={{ fontSize: 12 }}
                    >
                      {e.url}
                    </Typography.Text>
                  )}
                  {e.status === "error" && (
                    <Typography.Text type="danger" style={{ fontSize: 12 }}>
                      {e.error}
                    </Typography.Text>
                  )}
                </Flex>
                {statusIcon[e.status]}
              </Flex>
            ))}
          </Flex>
        )}
      </Flex>
    </Modal>
  );
}
