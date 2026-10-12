import { t } from "./i18n";
import { icon } from "./icons";

const escapeHTML = (value: string) => value.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

// The guide is local content and remains available without the desktop service.
export function guidePage() {
  const text = (key: string) => escapeHTML(t(key));
  const link = (page: string, label: string) => `<button type="button" class="button small quiet" data-page="${page}">${text(label)}${icon("arrow")}</button>`;
  const stages = [
    ["discover", "compass", "发现", "搜索 skills.sh，或预览 Git 仓库与本地目录，挑选技能导入。"],
    ["sources", "refresh", "来源", "追踪仓库与版本，刷新清单，再同步需要的技能。"],
    ["skills", "skills", "技能", "集中保存已安装技能，查看说明、来源和更新。"],
    ["presets", "layers", "预设", "把常用技能存成预设，在多个地方重复使用；也可直接选技能。"],
  ];
  const steps = [
    ["导入你的第一个技能", "在“发现”搜索、填写 Git 仓库或选择本地目录，预览后勾选技能并导入。导入只在本地保存技能。", "discover", "发现技能"],
    ["按需创建预设", "例如把代码审查和测试技能存为“日常开发”。在项目中优先选用预设，零散技能可展开“直接选择技能”补充。", "presets", "预设"],
    ["选择应用范围与目录", "通用技能放到“全局”；只为某个仓库配置时用“项目”，添加项目根目录，再勾选技能目录。", "global", "应用到全局"],
    ["选择后预览，再应用", "项目与全局的技能和预设选择会自动保存。预览各目录的新增、更新和移除，确认后点击应用，此时才会写入技能目录。", "projects", "应用到项目"],
  ];
  const questions = [
    ["刷新、同步、应用有什么区别？", "刷新读取仓库清单；同步把上游变化保存到本地技能；应用把所选技能写入全局或项目目录。更新本地技能后，仍需到对应页面预览并应用。"],
    ["挑选安装与整库订阅怎么选？", "挑选安装由你勾选需要的技能；整库订阅把扫描范围内的全部技能及后续新增纳入同步计划。两种方式都需要手动同步，全选导入不会自动开启订阅。"],
    ["同时选择技能和多个预设会怎样？", "最终使用直接选择与所有预设的技能并集，相同技能只保留一份。取消一个预设时，直接选择或其他预设仍需要的技能会保留。"],
    ["全局和项目会互相覆盖吗？", "它们分别管理所选目录。在项目中取消技能，不会停用 Agent 从全局目录读取的技能；不同 Agent 也可能读取同一个共享目录。"],
    ["复制和软链接有什么区别？", "默认复制会在应用目录保存独立副本；软链接指向本地技能的固定版本。两种方式在技能更新后都需重新应用，可在设置中切换。"],
    ["遇到冲突或上游删除会怎样？", "同名未托管内容或本地修改会阻止普通应用；可查看受影响路径并确认强制覆盖，覆盖或删除的内容无法恢复。重复技能名称等冲突仍需先处理。上游删除只标记状态并保留本地技能，可继续使用。备份与恢复在设置中管理。"],
  ];
  return `<article class="guide-page">
    <header class="page-heading"><div><h2 class="page-section-title">${text("指南")}</h2><p class="page-description">${text("了解技能从哪里来、如何组织，以及什么时候会写入应用目录。")}</p></div>${link("discover", "开始发现技能")}</header>
    <section class="panel guide-overview" aria-labelledby="guide-workflow-title">
      <div class="guide-section-heading"><span class="card-icon">${icon("skills")}</span><div><h3 id="guide-workflow-title">${text("racomi 如何工作")}</h3><p>${text("先把技能保存在本地，再按需应用到 Agent 使用的目录。")}</p></div></div>
      <ol class="guide-flow">${stages.map(([page, symbol, label, description], i) => `<li><button type="button" data-page="${page}" class="guide-stage"><span class="guide-stage-top"><span class="guide-stage-number">${i + 1}</span>${icon(symbol)}</span><strong>${text(label)}</strong><span>${text(description)}</span></button></li>`).join("")}</ol>
      <div class="guide-destinations"><span>${text("选择应用范围")}</span><div><button type="button" data-page="global">${icon("monitor")}<strong>${text("全局")}</strong><span>${text("供 Agent 在多个项目中使用")}</span>${icon("arrow")}</button><button type="button" data-page="projects">${icon("folder")}<strong>${text("项目")}</strong><span>${text("为指定项目配置技能目录")}</span>${icon("arrow")}</button></div></div>
      <p class="guide-note">${icon("info")}<span>${text("选择会自动保存；预览检查变化；点击应用才会修改目录。")}</span></p>
    </section>
    <section class="guide-start" aria-labelledby="guide-start-title"><h3 id="guide-start-title">${text("第一次使用")}</h3><ol class="guide-steps">${steps.map(([title, description, page, label], i) => `<li><span class="guide-step-number">${i + 1}</span><div><h4>${text(title)}</h4><p>${text(description)}</p></div>${link(page, label)}</li>`).join("")}</ol></section>
    <section class="guide-questions" aria-labelledby="guide-questions-title"><h3 id="guide-questions-title">${text("常见问题")}</h3><div class="panel">${questions.map(([question, answer]) => `<details><summary>${text(question)}</summary><p>${text(answer)}</p></details>`).join("")}</div></section>
  </article>`;
}
