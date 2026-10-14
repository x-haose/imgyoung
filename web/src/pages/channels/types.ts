/** 渠道模块共享类型,与后端 channel.Info 的 JSON 形态对齐。 */

/** ReplicaStats 渠道的副本补齐水位(按副本状态聚合)。 */
export interface ReplicaStats {
  done: number; // 已就绪副本数
  pending: number; // 待补齐副本数(含补齐进行中)
  failed: number; // 补齐失败副本数
}

/** FieldOption select 字段的一个选项。 */
export interface FieldOption {
  label: string;
  value: string;
}

/** TemplateField 模板的一个用户输入字段。 */
export interface TemplateField {
  key: string;
  label: string;
  type: "text" | "password" | "select" | "oauth";
  required: boolean;
  placeholder?: string;
  help?: string;
  options?: FieldOption[];
}

/** ChannelTemplate 渠道模板(后端下发,不含 provider 等预设)。 */
export interface ChannelTemplate {
  id: string;
  name: string;
  description: string;
  driver: string;
  category: string; // 分类,与后端 channel.Category* 常量对齐
  requires_oauth: boolean;
  fields: TemplateField[];
}

/** ChannelItem 渠道列表项(凭证与配置不出库)。 */
export interface ChannelItem {
  id: number;
  name: string;
  driver: string;
  priority: number;
  healthy: boolean;
  encrypted: boolean;
  allow_direct_link: boolean;
  last_checked_at: string; // 最近探测时刻,UTC 文本;空 = 尚未探测
  last_error: string; // 最近探测失败原因(人话化);空 = 最近一次探测成功
  deleting: boolean; // 清理中:排空任务正在删除渠道上的对象,清完即消失
  replicas: ReplicaStats; // 副本补齐水位
}
