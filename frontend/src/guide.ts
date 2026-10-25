import { t } from "./i18n";
import { icon } from "./icons";

const escapeHTML = (value: string) => value.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
const text = (key: string) => escapeHTML(t(key));
const link = (page: string, label: string) => `<button type="button" class="button small quiet" data-page="${page}">${text(label)}${icon("arrow")}</button>`;
export const guideTabs = [
  { id: "start", label: "使用入门" },
  { id: "sync", label: "同步与备份" },
  { id: "faq", label: "常见问题" },
] as const;
export type GuideTab = typeof guideTabs[number]["id"];

// The guide is local content and remains available without the desktop service.
export function guidePage(activeTab: GuideTab = "start") {
  const stages = [
    ["discover", "compass", "发现"],
    ["sources", "refresh", "来源"],
    ["skills", "skills", "技能"],
    ["presets", "layers", "预设"],
  ];
  const steps = [
    ["导入你的第一个技能", "在“发现”搜索、填写 Git 仓库或选择本地目录，预览后勾选技能并导入。导入只在本地保存技能。", "discover", "发现技能"],
    ["按需创建预设", "例如把代码审查和测试技能存为“日常开发”。在项目中优先选用预设，零散技能可展开“直接选择技能”补充。", "presets", "预设"],
    ["选择应用范围与目录", "通用技能放到“全局”；只为某个仓库配置时用“项目”，添加项目根目录，再勾选技能目录。", "global", "应用到全局"],
    ["选择后预览，再应用", "项目与全局的技能、预设和来源选择会自动保存。预览各目录的新增、更新和移除，确认后点击应用，此时才会写入技能目录。", "projects", "应用到项目"],
  ];
  const questions = [
    ["刷新、同步、应用有什么区别？", "刷新读取仓库清单；同步把上游变化保存到本地技能；应用把所选技能写入全局或项目目录。更新本地技能后，仍需到对应页面预览并应用。"],
    ["挑选安装与全部订阅怎么选？", "挑选安装由你勾选需要的技能；全部订阅把扫描范围内的全部技能及后续新增纳入同步计划。两种方式都需要手动同步，全选导入不会自动开启订阅。"],
    ["同时选择技能、预设和来源会怎样？", "最终使用直接选择、所有预设和所选来源已安装技能的并集，相同技能只保留一份。取消一项选择时，其他选择仍需要的技能会保留。来源后续安装的新技能也会纳入，仍需预览并手动应用。"],
    ["全局和项目会互相覆盖吗？", "它们分别管理所选目录。在项目中取消技能，不会停用 Agent 从全局目录读取的技能；不同 Agent 也可能读取同一个共享目录。"],
    ["复制和软链接有什么区别？", "默认复制会在应用目录保存独立副本；软链接指向本地技能的固定版本。两种方式在技能更新后都需重新应用，可在设置中切换。"],
    ["遇到冲突或上游删除会怎样？", "同名未托管内容或本地修改会阻止普通应用；可查看受影响路径并确认强制覆盖，覆盖或删除的内容无法恢复。重复技能名称等冲突仍需先处理。上游删除只标记状态并保留本地技能，可继续使用。备份与恢复在设置中管理。"],
  ];
  const title = guideTabs.find((tab) => tab.id === activeTab)?.label || guideTabs[0].label;
  return `<article class="guide-page">
    <header class="page-heading"><div><h2 class="page-section-title">${text(title)}</h2></div>${activeTab === "start" ? link("discover", "开始发现技能") : ""}</header>
    ${activeTab === "start" ? `
    <section class="panel guide-overview" aria-labelledby="guide-workflow-title">
      <div class="guide-section-heading"><span class="card-icon">${icon("skills")}</span><div><h3 id="guide-workflow-title">${text("racomi 如何工作")}</h3></div></div>
      <ol class="guide-flow">${stages.map(([page, symbol, label], i) => `<li><button type="button" data-page="${page}" class="guide-stage"><span class="guide-stage-top"><span class="guide-stage-number">${i + 1}</span>${icon(symbol)}</span><strong>${text(label)}</strong></button></li>`).join("")}</ol>
      <div class="guide-destinations"><span>${text("选择应用范围")}</span><div><button type="button" data-page="global">${icon("monitor")}<strong>${text("全局")}</strong><span>${text("供 Agent 在多个项目中使用")}</span>${icon("arrow")}</button><button type="button" data-page="projects">${icon("folder")}<strong>${text("项目")}</strong><span>${text("为指定项目配置技能目录")}</span>${icon("arrow")}</button></div></div>
      <p class="guide-note">${icon("info")}<span>${text("选择会自动保存；预览检查变化；点击应用才会修改目录。")}</span></p>
    </section>
    <section class="guide-start" aria-labelledby="guide-start-title"><h3 id="guide-start-title">${text("第一次使用")}</h3><ol class="guide-steps">${steps.map(([title, description, page, label], i) => `<li><span class="guide-step-number">${i + 1}</span><div><h4>${text(title)}</h4><p>${text(description)}</p></div>${link(page, label)}</li>`).join("")}</ol></section>
    ` : activeTab === "sync" ? cloudSyncGuide() : `<section class="guide-questions" aria-labelledby="guide-questions-title"><h3 id="guide-questions-title" class="sr-only">${text("常见问题")}</h3><div class="panel">${questions.map(([question, answer]) => `<details><summary>${text(question)}</summary><p>${text(answer)}</p></details>`).join("")}</div></section>`}
  </article>`;
}

function cloudSyncGuide() {
  return `<section class="guide-cloud" aria-labelledby="guide-cloud-title"><div class="guide-section-heading"><span class="card-icon">${icon("refresh")}</span><div><h3 id="guide-cloud-title">${text("S3 / R2 sync and backup")}</h3></div><button type="button" class="button small quiet" data-action="open-cloud-settings">${text("Open sync settings")}${icon("arrow")}</button></div>
    <div class="panel guide-cloud-content">
      <p>${text("Two-way sync includes sources, skill files, presets, theme, language, navigation layout, and shared deployment defaults. Project roots, custom absolute paths, deployment records, caches, and storage credentials stay on each device. Sync never applies skills to Agent directories automatically.")}</p>
      <details open><summary>${text("Cloudflare R2 with browser authorization")}</summary><ol>
        <li>${text("Activate R2 and create a private bucket in Cloudflare. Your account needs permission to provision Account API Tokens and read and write the chosen bucket.")}</li>
        <li>${text("In sync settings, choose Cloudflare R2 → Connect Cloudflare. Complete authorization in your browser, select the account and jurisdiction, choose a bucket, then select Use this bucket. racomi creates and manages bucket-scoped credentials without asking you to copy access keys.")}</li>
        <li>${text("Enter the same object prefix and encryption passphrase used on your other devices. Test the connection, save the settings, then choose Sync now. The sync, backup history, encryption, and conflict handling are the same as Manual S3.")}</li>
      </ol><p>${text("Cloudflare R2 and Manual S3 keep separate saved configurations. Switching the option edits a draft; saving changes the active method. Canceling keeps the current method. If Cloudflare authorization is unavailable in this build, use Manual S3.")}</p><p>${text("Disconnect Cloudflare removes the credentials created by racomi and revokes its authorization; the private bucket and its encrypted data are kept.")}</p></details>
      <details><summary>${text("Cloudflare R2 with Manual S3 configuration")}</summary><ol>
        <li>${text("Create a private R2 bucket. In R2 → Manage API tokens, create a token with Object Read & Write permission scoped to that bucket. Copy its Access Key ID and Secret Access Key; do not paste the Cloudflare API token value into these fields.")}</li>
        <li>${text("In Settings → Backup and migration → S3 / R2 sync, enter the following values. Use the jurisdiction endpoint shown by Cloudflare if your bucket has a jurisdiction restriction.")}<div class="guide-cloud-table"><table><tbody><tr><th>${text("S3 endpoint")}</th><td><code>https://&lt;ACCOUNT_ID&gt;.r2.cloudflarestorage.com</code></td></tr><tr><th>${text("Bucket")}</th><td>${text("Your private bucket name")}</td></tr><tr><th>${text("Region")}</th><td><code>auto</code></td></tr><tr><th>${text("Object prefix")}</th><td><code>racomi</code> ${text("(or a prefix you choose)")}</td></tr><tr><th>${text("Use path-style addressing")}</th><td>${text("Enabled")}</td></tr></tbody></table></div></li>
        <li>${text("Enter the access keys and an encryption passphrase of at least 12 characters. Save a copy of the passphrase securely. Test the connection, save the settings, then choose Sync now.")}</li>
      </ol><p><a href="https://developers.cloudflare.com/r2/api/tokens/" target="_blank" rel="noopener noreferrer">${text("R2 credentials documentation")}</a> · <a href="https://developers.cloudflare.com/r2/api/s3/api/" target="_blank" rel="noopener noreferrer">${text("R2 S3 API documentation")}</a></p></details>
      <details><summary>${text("AWS S3 and compatible storage")}</summary><p>${text("Create a private bucket and credentials limited to that bucket. For AWS S3, use https://s3.<REGION>.amazonaws.com and the actual bucket region. Disable path-style addressing for virtual-hosted requests. For other services, use their S3 API endpoint, region, and addressing mode. Temporary credentials also need their session token.")}</p><p>${text("Grant s3:ListBucket on the bucket and s3:GetObject, s3:PutObject, and s3:DeleteObject on the chosen prefix. DeleteObject lets the connection test clean up its probe; normal sync retains historical versions. The storage service must support conditional writes for safe concurrent sync.")}</p><p>${text("Keep the bucket private. Desktop sync runs through the local Go backend, so public bucket access and browser CORS rules are unnecessary.")}</p><p><a href="https://docs.aws.amazon.com/AmazonS3/latest/userguide/access-bucket-intro.html" target="_blank" rel="noopener noreferrer">${text("AWS S3 access documentation")}</a></p></details>
      <details><summary>${text("Connect another device and handle conflicts")}</summary><p>${text("On every device, configure the same endpoint, bucket, object prefix, and encryption passphrase. The endpoint is part of the encryption identity, so use the same hostname too. Each device may use its own bucket-scoped access keys. The first sync merges existing local and cloud libraries; different edits to the same record require a choice. Separate prefixes create separate libraries.")}</p><p>${text("Later syncs compare changes with the last shared version: independent edits merge, deletions propagate, and conflicting edits or edit-versus-delete changes require review. Review each local and cloud version and choose one before continuing. A cloud deletion is different from an upstream repository removing a skill, which still preserves its local copy.")}</p><p>${text("Automatic sync is optional and runs at the configured interval while racomi is open. Offline edits remain usable and sync after the connection returns. Configure local project and global paths separately, then preview and apply updated skills manually.")}</p></details>
      <details><summary>${text("History, recovery, and connection problems")}</summary><p>${text("Settings → Backup and migration → S3 / R2 sync → Cloud backups lists the latest 100 committed versions. Restoring creates a local snapshot first, then publishes the selected library as a new sync version. Other devices receive that restoration on their next sync. History is retained without automatic pruning; do not add bucket lifecycle rules that delete active sync objects.")}</p><p>${text("For AccessDenied, check the bucket, prefix, and object permissions. For signature errors, check the endpoint, region, access key pair, session token, and device clock. A decryption error usually means a different passphrase or prefix. Saved secrets stay blank in the form; leaving them blank preserves them. Enter a new secret when changing the endpoint or access key.")}</p><p>${text("Cloudflare authorization and generated R2 credentials are kept in the operating-system credential store. Manual S3 credentials and the encryption passphrase stay in this device’s private configuration file. None are synced or included in ZIP backups. Keep a separate copy of the passphrase: the storage service cannot recover it. To use a new passphrase, create a separate prefix and configure every device for it.")}</p></details>
    </div></section>`;
}
