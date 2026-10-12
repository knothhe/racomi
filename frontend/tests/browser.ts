// An isolated browser fixture. All mutations affect this in-memory workspace;
// no Wails service, user's skills or deployment directory is accessed.
import { api, type AppState, type ApplyPlan, type Target } from "../src/api";

const state: AppState = {
  dataDir: "/test/racomi", homeDir: "/test/home", directoryOptions: [], snapshots: [], projects: [],
  globalApplications: [
    { key: "codex-user", agentId: "agents", agentName: "共享", name: "用户技能目录", path: "/test/home/.agents/skills", description: "Codex 与 Cursor 共用的本机用户技能目录，适用于所有项目。", target: null },
    { key: "codex-legacy", agentId: "codex", agentName: "Codex", name: "兼容技能目录", path: "/test/home/.codex/skills", description: "保留已有 Codex 技能配置，遵循 CODEX_HOME；新配置建议使用 ~/.agents/skills。", target: null },
    { key: "claude-user", agentId: "claude", agentName: "Claude Code", name: "用户技能目录", path: "/test/home/.claude/skills", description: "Claude Code 的本机用户技能目录，Cursor 也可兼容读取。", target: null },
    { key: "cursor-user", agentId: "cursor", agentName: "Cursor", name: "用户技能目录", path: "/test/home/.cursor/skills", description: "Cursor 的本机用户技能目录，适用于所有项目。", target: null },
  ],
  state: {
    schema: 2, settings: { deploymentMode: "copy", defaultProjectDirectories: ["agents"], defaultGlobalDirectories: ["codex-user"], customProjectDirectories: [] },
    skills: [
      { id: "design", name: "Design Skill", description: "Design interface", source: "https://example.com/team/design", sourceId: "source-design" },
      { id: "code", name: "Code Skill", description: "Code review", source: "https://example.com/team/code", sourceId: "source-code" },
      { id: "local", name: "Local Skill", description: "Local independent skill", source: "", sourceId: "" },
      { id: "user-text", name: "名称", description: "设置", source: "", sourceId: "" },
    ].map((skill) => ({ ref: "main", subpath: ".", revision: "0123456789", contentHash: skill.id, installedAt: "2026-10-11T00:00:00Z", updatedAt: "2026-10-11T00:00:00Z", upstreamRemoved: false, ...skill })),
    sources: [
      { id: "source-design", name: "Design source", repository: "https://example.com/team/design" },
      { id: "source-code", name: "Code source", repository: "https://example.com/team/code" },
    ].map((source) => ({ ref: "main", subpath: ".", mode: "selected" as const, revision: "", refreshedAt: "", lastError: "", catalog: [], ...source })),
    presets: [{ id: "preset-dev", name: "Development", description: "Design and code", skillIds: ["design", "code"] }], targets: [],
  },
};

const testParameters = new URLSearchParams(location.search);
let projectSelectionFailures = testParameters.has("project-selection-fail-once") ? 1 : 0;
// Opt in to realistic, populated pages without changing regression-test fixtures.
const visualReview = testParameters.has("visual-review");
if (visualReview && testParameters.has("native-mac")) document.body.classList.add("native-mac");
if (visualReview) {
  state.dataDir = "/test/home/Library/Application Support/Racomi/workspaces/product-design-team";
  state.directoryOptions = [
    { id: "agents", name: "共享技能目录", path: ".agents/skills", kind: "project", description: "Codex 与 Cursor 共用的项目技能目录。" },
    { id: "claude", name: "Claude Code", path: ".claude/skills", kind: "project", description: "Claude Code 的项目技能目录，Cursor 也可兼容读取。" },
    { id: "codex", name: "Codex（兼容）", path: ".codex/skills", kind: "project", description: "保留已有 .codex/skills 配置；新项目建议使用 .agents/skills。" },
    { id: "cursor", name: "Cursor", path: ".cursor/skills", kind: "project", description: "Cursor 的项目技能目录。" },
  ];
  state.state.settings.customProjectDirectories = [
    { id: "project-dir-team", name: "团队共享技能 / Team shared skills", path: "tools/ai-assistants/shared/skills" },
    { id: "project-dir-review", name: "代码审查工作流", path: ".review/automation/skills" },
  ];
  state.state.settings.defaultProjectDirectories = ["agents", "claude", "project-dir-team"];
  state.state.settings.defaultGlobalDirectories = ["codex-user", "claude-user"];
  const entries = [
    { id: "design", name: "视觉对齐与界面设计 / Interface Design", description: "检查网格、留白、字重与视觉层级，让复杂工作流在中文与英文界面中保持清晰、一致的阅读节奏。", sourceId: "source-design", subpath: "skills/interface-design" },
    { id: "code", name: "Production-ready Code Review", description: "Review correctness, accessibility, maintainability, and release risks with actionable examples and focused validation.", sourceId: "source-code", subpath: "skills/code-review" },
    { id: "local", name: "团队写作规范", description: "本地维护的产品文案与技术文档约定，覆盖命名、标点、错误提示和中英文混排。", sourceId: "", subpath: "." },
    { id: "user-text", name: "名称", description: "设置", sourceId: "", subpath: "." },
    { id: "accessibility", name: "Accessibility and Inclusive Interaction Patterns", description: "Audit keyboard navigation, focus visibility, semantic labels, contrast, and responsive interfaces across desktop and compact windows.", sourceId: "source-design", subpath: "skills/accessibility" },
    { id: "research", name: "用户研究与产品发现：从访谈记录到可验证的设计决策", description: "整理长篇访谈与研究笔记，提炼用户目标、关键任务和设计机会，并生成供跨职能团队讨论的清晰摘要。", sourceId: "source-design", subpath: "skills/product-discovery" },
    { id: "testing", name: "Browser Regression Testing", description: "Plan repeatable browser checks for dialogs, forms, deployment previews, and multilingual layouts.", sourceId: "source-code", subpath: "skills/browser-testing" },
    { id: "legacy", name: "Legacy Migration Assistant", description: "保留的旧版迁移技能；上游已移除，本地副本仍可用于维护现有项目。", sourceId: "source-legacy", subpath: "skills/legacy-migration" },
  ];
  state.state.skills = entries.map((entry) => ({
    ...entry, source: entry.sourceId ? `https://example.com/team/${entry.sourceId.replace("source-", "")}` : "",
    ref: "main", revision: "8f2b4cd96a10", contentHash: `visual-${entry.id}`,
    installedAt: "2026-09-21T06:30:00Z", updatedAt: "2026-10-11T09:15:00Z", upstreamRemoved: entry.id === "legacy",
  }));
  state.state.sources = [
    { id: "source-design", name: "产品设计与体验规范 / Product Design", repository: "https://example.com/team/design", mode: "selected" as const, lastError: "" },
    { id: "source-code", name: "Engineering workflows", repository: "https://example.com/team/code", mode: "all" as const, lastError: "" },
    { id: "source-legacy", name: "历史迁移工具与兼容性指南", repository: "https://example.com/team/legacy", mode: "selected" as const, lastError: "Unable to refresh repository: the remote is temporarily unavailable." },
  ].map((source) => ({
    ...source, ref: "main", subpath: "skills", revision: "9e8d7c6b5a40", refreshedAt: "2026-10-12T02:20:00Z",
    catalog: state.state.skills.filter((skill) => skill.sourceId === source.id).map((skill) => ({
      name: skill.name, description: skill.description, subpath: skill.subpath, revision: "9e8d7c6b5a40",
      contentHash: skill.id === "design" ? "visual-design-update" : skill.contentHash, removed: skill.upstreamRemoved, new: false,
    })),
  }));
  state.state.sources[0].catalog.push(
    { name: "Design Tokens and Component Libraries", description: "Document a shared visual language for reusable components, spacing, typography, and semantic colors.", subpath: "skills/design-tokens", revision: "9e8d7c6b5a40", contentHash: "visual-tokens", removed: false, new: true },
    { name: "Motion / 交互动效", description: "包含不支持的仓库内容，用于检查来源警告与跳过状态的布局。", subpath: "skills/motion", revision: "9e8d7c6b5a40", contentHash: "visual-motion", removed: false, new: true, skipReason: 'unsupported skill content at "skills/motion/assets": symlink is not supported' },
  );
  state.state.sources[0].warnings = ['skipped repository entry "skills/motion/assets": symlink is not supported'];
  state.state.presets = [
    { id: "preset-dev", name: "产品研发 / Product Development", description: "从设计评审到代码交付，为日常迭代准备的一组协作技能。", skillIds: ["design", "code", "testing"] },
    { id: "preset-design", name: "Design System & Accessibility Review", description: "保持界面视觉一致性，兼顾键盘操作与不同窗口宽度下的阅读体验。", skillIds: ["design", "accessibility", "research", "local"] },
    { id: "preset-maintenance", name: "历史项目维护与迁移", description: "面向长期维护项目的审查与兼容性迁移工作流。", skillIds: ["code", "legacy"] },
  ];
  const deployments = (ids: string[], paths: string[]) => paths.flatMap((path) => ids.map((id) => {
    const skill = state.state.skills.find((item) => item.id === id)!;
    return { skillId: id, name: skill.name, contentHash: skill.contentHash, path, mode: "copy" as const };
  }));
  const productRoot = "/test/home/Workspace/Product Platform/customer-experience-console";
  const designRoot = "/test/home/Workspace/Design Systems/multilingual-component-library";
  state.projects = [
    { id: "project-product", name: "客户体验工作台 / Customer Experience", kind: "project", projectRoot: productRoot, directoryIds: ["agents", "claude"], customPaths: ["tools/ai-assistants/shared/skills"], paths: [`${productRoot}/.agents/skills`, `${productRoot}/.claude/skills`, `${productRoot}/tools/ai-assistants/shared/skills`], path: `${productRoot}/.agents/skills`, enabledSkillIds: ["accessibility"], enabledPresetIds: ["preset-dev"], deployments: deployments(["design", "code"], [`${productRoot}/.agents/skills`, `${productRoot}/.claude/skills`]), pending: true },
    { id: "project-design", name: "Multilingual Component Library", kind: "project", projectRoot: designRoot, directoryIds: ["agents", "cursor"], customPaths: [], paths: [`${designRoot}/.agents/skills`, `${designRoot}/.cursor/skills`], path: `${designRoot}/.agents/skills`, enabledSkillIds: [], enabledPresetIds: ["preset-design"], deployments: deployments(["design", "accessibility", "research", "local"], [`${designRoot}/.agents/skills`, `${designRoot}/.cursor/skills`]), pending: false },
    { id: "project-research", name: "产品探索与研究资料", kind: "project", projectRoot: "/test/home/Workspace/Research Notes", directoryIds: ["agents"], customPaths: [], paths: ["/test/home/Workspace/Research Notes/.agents/skills"], path: "/test/home/Workspace/Research Notes/.agents/skills", enabledSkillIds: ["research", "local"], enabledPresetIds: [], deployments: [], pending: true },
  ];
  for (const application of state.globalApplications.filter((item) => ["codex-user", "claude-user", "cursor-user"].includes(item.key))) {
    const pending = application.key === "claude-user";
    const ids = application.key === "cursor-user" ? ["local"] : ["design", "code", "testing"];
    application.target = { id: application.key, name: application.agentName, kind: "global", path: application.path, paths: [application.path], enabledSkillIds: application.key === "cursor-user" ? ["local"] : [], enabledPresetIds: application.key === "cursor-user" ? [] : ["preset-dev"], deployments: deployments(pending ? ["code"] : ids, [application.path]), pending };
  }
  state.state.targets = [...state.projects, ...state.globalApplications.flatMap((application) => application.target ? [application.target] : [])];
  state.snapshots = [
    { id: "snapshot-visual-3", createdAt: "2026-10-12T02:30:00Z", reason: "", skillCount: 8, presetCount: 3 },
    { id: "snapshot-visual-2", createdAt: "2026-10-11T09:15:00Z", reason: "before installing or updating skills", skillCount: 8, presetCount: 3 },
    { id: "snapshot-visual-1", createdAt: "2026-10-09T06:00:00Z", reason: "团队设计规范升级前 / Before design system migration", skillCount: 6, presetCount: 2 },
  ];
}
if (testParameters.has("partial-source")) {
  const source = state.state.sources[0];
  source.name = "Partial source";
  source.revision = "1234567890abcdef";
  source.refreshedAt = "2026-10-12T00:00:00Z";
  source.warnings = ['skipped repository entry "AGENTS.md": symlink is not supported'];
  if (testParameters.has("all")) source.mode = "all";
  const design = state.state.skills[0], code = state.state.skills[1];
  design.subpath = "skills/design";
  code.sourceId = source.id; code.source = source.repository; code.subpath = "skills/code";
  source.catalog = [
    { name: design.name, description: design.description, subpath: design.subpath, revision: design.revision, contentHash: design.contentHash, removed: false, new: false, skipReason: 'unsupported skill content at "skills/design/AGENTS.md": symlink is not supported' },
    { name: code.name, description: code.description, subpath: code.subpath, revision: source.revision, contentHash: "updated-code", removed: false, new: false },
    { name: "New Skill", description: "A valid new skill", subpath: "skills/new", revision: source.revision, contentHash: "new-skill", removed: false, new: true },
  ];
}
if (testParameters.has("legacy-template")) {
  state.state.settings.customProjectDirectories = [{ id: "project-dir-legacy", name: "Legacy absolute directory", path: "/test/shared/skills" }];
  state.state.settings.defaultProjectDirectories = ["project-dir-legacy"];
}
if (testParameters.has("legacy-project")) {
  const project: Target = { id: "project-legacy", name: "Legacy project", kind: "project", projectRoot: "/test/legacy-project", directoryIds: [], customPaths: [".legacy/skills", "/test/shared/skills"], path: "/test/legacy-project/.legacy/skills", paths: ["/test/legacy-project/.legacy/skills", "/test/shared/skills"], enabledSkillIds: [], enabledPresetIds: [], deployments: [], pending: false };
  state.projects.push(project); state.state.targets.push(project);
}
if (testParameters.has("projects") || testParameters.has("many-projects")) {
  const requested = Number(testParameters.get("projects") || 40);
  const count = Math.max(1, Math.min(250, Number.isFinite(requested) ? Math.floor(requested) : 40));
  for (let index = state.projects.length; index < count; index++) {
    const number = String(index + 1).padStart(2, "0");
    const root = `/test/home/Workspace/Products/service-${number}`;
    const paths = [`${root}/.agents/skills`, ...(index % 3 === 0 ? [`${root}/.claude/skills`] : [])];
    const skillIDs = index % 2 === 0 ? ["design", "local"] : ["code"];
    const presetIDs = index % 4 === 1 ? ["preset-dev"] : [];
    const effective = new Set([...skillIDs, ...state.state.presets.filter((preset) => presetIDs.includes(preset.id)).flatMap((preset) => preset.skillIds)]);
    const pending = index % 3 === 0;
    const project: Target = { id: `project-fixture-${index + 1}`, name: `Project ${number} · ${index % 2 === 0 ? "客户服务" : "Developer Tools"}`, kind: "project", projectRoot: root, path: paths[0], paths, directoryIds: index % 3 === 0 ? ["agents", "claude"] : ["agents"], customPaths: [], enabledSkillIds: skillIDs, enabledPresetIds: presetIDs, pending, deployments: pending ? [] : paths.flatMap((path) => state.state.skills.filter((skill) => effective.has(skill.id)).map((skill) => ({ skillId: skill.id, name: skill.name, contentHash: skill.contentHash, path, mode: "copy" }))) };
    state.projects.push(project); state.state.targets.push(project);
  }
}
if (testParameters.has("skills")) {
  const requested = Number(testParameters.get("skills"));
  const count = Math.max(0, Math.min(600, Number.isFinite(requested) ? Math.floor(requested) : 60));
  const template = state.state.skills.find((skill) => !skill.sourceId) || state.state.skills[0];
  for (let index = state.state.skills.length; index < count; index++) {
    const number = String(index + 1).padStart(3, "0");
    state.state.skills.push({
      ...template, id: `skill-fixture-${index + 1}`, name: index % 2 === 0 ? `Skill ${number} · 团队工作流` : `Skill ${number} · Accessibility and Production Review`,
      description: `Fixture skill ${number}: 用于检查长列表滚动、搜索和技能详情。`, contentHash: `fixture-skill-${index + 1}`,
      source: "", sourceId: "", subpath: ".", upstreamRemoved: false,
    });
  }
  if (count < state.state.skills.length) state.state.skills.length = count;
  const available = new Set(state.state.skills.map((skill) => skill.id));
  for (const preset of state.state.presets) preset.skillIds = preset.skillIds.filter((id) => available.has(id));
  for (const target of state.state.targets) {
    target.enabledSkillIds = target.enabledSkillIds.filter((id) => available.has(id));
    target.deployments = target.deployments.filter((deployment) => available.has(deployment.skillId));
  }
}
if (testParameters.has("sources")) {
  const requested = Number(testParameters.get("sources"));
  const count = Math.max(0, Math.min(100, Number.isFinite(requested) ? Math.floor(requested) : 40));
  const template = state.state.sources[0];
  for (let index = state.state.sources.length; index < count; index++) {
    const number = String(index + 1).padStart(3, "0");
    state.state.sources.push({
      ...template, id: `source-fixture-${index + 1}`, name: index % 2 === 0 ? `Source ${number} · 团队技能仓库` : `Source ${number} · Engineering Workflows`,
      repository: `https://example.com/fixtures/source-${number}`, mode: index % 2 === 0 ? "selected" : "all",
      revision: `fixture-source-${index + 1}`, refreshedAt: "2026-10-12T00:00:00Z", lastError: "", catalog: [], warnings: [],
    });
  }
  if (count < state.state.sources.length) state.state.sources.length = count;
  const available = new Set(state.state.sources.map((source) => source.id));
  for (const skill of state.state.skills) if (skill.sourceId && !available.has(skill.sourceId)) {
    skill.sourceId = ""; skill.source = "";
  }
}
if (testParameters.has("presets")) {
  const requested = Number(testParameters.get("presets"));
  const count = Math.max(0, Math.min(100, Number.isFinite(requested) ? Math.floor(requested) : 30));
  for (let index = state.state.presets.length; index < count; index++) {
    const number = String(index + 1).padStart(2, "0");
    state.state.presets.push({ id: `preset-fixture-${index + 1}`, name: index % 3 === 0 ? `代码审查 ${number}` : index % 3 === 1 ? `Accessibility & Design System Review ${number}` : `文档与发布 ${number}`, description: "适用于团队协作的预设，包含常用技能。", skillIds: state.state.skills.slice(0, index % state.state.skills.length + 1).map((skill) => skill.id) });
  }
  if (count < state.state.presets.length) state.state.presets.length = count;
  const available = new Set(state.state.presets.map((preset) => preset.id));
  for (const target of state.state.targets) target.enabledPresetIds = target.enabledPresetIds.filter((id) => available.has(id));
}
const settingsDelay = Math.min(2000, Math.max(0, Number(testParameters.get("settings-delay")) || 0));
let settingsFailure = testParameters.has("settings-fail-once");
if (testParameters.has("blocking-conflict")) {
  const template = state.state.skills[0];
  const duplicateIDs = ["force-duplicate-first", "force-duplicate-second"];
  state.state.skills.push(...duplicateIDs.map((id) => ({ ...template, id, name: "Duplicate Deployment Skill", source: "", sourceId: "", subpath: ".", contentHash: id })));
  for (const target of state.state.targets) target.enabledSkillIds = [...new Set([...target.enabledSkillIds, ...duplicateIDs])];
}
if (testParameters.has("force-conflict") || testParameters.has("blocking-conflict")) {
  for (const target of state.state.targets) target.pending = true;
}
const copy = () => structuredClone(state);
const checkedSkills = (ids: string[]) => [...new Set(ids)].map((id) => {
  const skill = state.state.skills.find((item) => item.id === id);
  if (!skill) throw new Error("skill not found");
  return skill;
});
const updateSkills = async (ids: string[]) => {
  const selected = checkedSkills(ids);
  if (selected.some((skill) => !state.state.sources.some((source) => source.id === skill.sourceId))) throw new Error("skill source is unavailable");
  for (const skill of selected) {
    skill.revision = `updated-${skill.id}`;
    skill.contentHash = `updated-${skill.id}`;
    skill.updatedAt = "2026-10-12T00:00:00Z";
  }
  document.documentElement.dataset.updatedSkillIds = selected.map((skill) => skill.id).join(",");
  return copy();
};
const removeSkills = async (ids: string[]) => {
  const selected = checkedSkills(ids);
  if (selected.some((skill) => state.state.presets.some((preset) => preset.skillIds.includes(skill.id)) || state.state.targets.some((target) => target.enabledSkillIds.includes(skill.id)))) throw new Error("skill is still in use");
  const removed = new Set(selected.map((skill) => skill.id));
  state.state.skills = state.state.skills.filter((skill) => !removed.has(skill.id));
  document.documentElement.dataset.removedSkillIds = [...removed].join(",");
  return copy();
};
const selectedSkills = (target: Target) => new Set([...target.enabledSkillIds, ...state.state.presets.filter((preset) => target.enabledPresetIds.includes(preset.id)).flatMap((preset) => preset.skillIds)]);
const forcedDirectories = new Set<string>();
const directoryKey = (target: Target, path: string) => `${target.id}:${path}`;
const recordApplyCall = (method: string) => {
  const key = `${method}Calls`;
  document.documentElement.dataset[key] = String(Number(document.documentElement.dataset[key] || 0) + 1);
};
for (const method of ["applyProject", "applyGlobal", "applyGlobals", "forceApplyProject", "forceApplyGlobal", "forceApplyGlobals"]) document.documentElement.dataset[`${method}Calls`] = "0";
document.documentElement.dataset.forceApplyCalls = "0";
const plan = (targets: Target[]): ApplyPlan & { overwrite: string[]; blockingConflicts: string[] } => {
  const directories = targets.flatMap((target) => [...new Set([...(target.paths || [target.path]), ...target.deployments.map((item) => item.path || target.path)])].map((path) => {
    const desired = (target.paths || [target.path]).includes(path) ? state.state.skills.filter((skill) => selectedSkills(target).has(skill.id)) : [];
    const deployed = target.deployments.filter((item) => (item.path || target.path) === path);
    const names = new Map<string, string>();
    const blockingConflicts: string[] = [];
    for (const skill of desired) {
      const other = names.get(skill.name.toLowerCase());
      if (other) blockingConflicts.push(`${skill.name}: multiple skills have the same deployment name (${other})`);
      else names.set(skill.name.toLowerCase(), skill.id);
    }
    const conflicts = [...blockingConflicts];
    const overwrite: string[] = [];
    // Simulate a locally edited managed skill (or an unmanaged incoming skill)
    // at the same absolute deployment path the confirmation must display.
    const conflictSkill = desired.find((skill) => !skill.name.includes("/")) || deployed.find((skill) => !skill.name.includes("/"));
    const forceConflict = testParameters.has("force-conflict") && !forcedDirectories.has(directoryKey(target, path)) && !!conflictSkill;
    if (forceConflict) {
      conflicts.push(`${conflictSkill!.name}: locally modified; existing content was preserved`);
      overwrite.push(`${path}/${conflictSkill!.name}`);
    }
    return {
      targetId: target.id, path,
      add: desired.filter((skill) => !deployed.some((item) => item.skillId === skill.id)).map((skill) => skill.name),
      update: desired.filter((skill) => deployed.some((item) => item.skillId === skill.id && (item.contentHash !== skill.contentHash || (item.mode || "copy") !== state.state.settings.deploymentMode || forceConflict && skill.name === conflictSkill?.name))).map((skill) => skill.name),
      remove: deployed.filter((item) => !desired.some((skill) => skill.id === item.skillId)).map((item) => item.name), conflicts, blockingConflicts, overwrite,
      fingerprint: `fixture:${directoryKey(target, path)}:${forcedDirectories.has(directoryKey(target, path)) ? "applied" : "initial"}`,
    };
  }));
  return { targetId: targets[0]?.id || "", directories, add: [...new Set(directories.flatMap((directory) => directory.add))], update: [...new Set(directories.flatMap((directory) => directory.update))], remove: [...new Set(directories.flatMap((directory) => directory.remove))], conflicts: directories.flatMap((directory) => directory.conflicts.map((conflict) => `${directory.path}: ${conflict}`)), blockingConflicts: directories.flatMap((directory) => directory.blockingConflicts.map((conflict) => `${directory.path}: ${conflict}`)), overwrite: [...new Set(directories.flatMap((directory) => directory.overwrite))] };
};
const apply = (targets: Target[], force = false, reviewed?: ApplyPlan) => {
  const current = plan(targets);
  if (current.blockingConflicts.length || (!force && current.conflicts.length)) throw new Error(`application has conflicts: ${(force ? current.blockingConflicts : current.conflicts).join("; ")}`);
  if (force) {
    if (!reviewed) throw new Error("review the application plan before forcing an overwrite");
    document.documentElement.dataset.forceOverwritePaths = JSON.stringify(current.overwrite);
    for (const directory of current.directories || []) forcedDirectories.add(`${directory.targetId}:${directory.path}`);
    document.documentElement.dataset.forceAppliedTargets = targets.map((target) => target.id).join(",");
    document.documentElement.dataset.forceApplyCalls = String(Number(document.documentElement.dataset.forceApplyCalls || 0) + 1);
  }
  for (const target of targets) {
    target.deployments = (target.paths || [target.path]).flatMap((path) => state.state.skills.filter((skill) => selectedSkills(target).has(skill.id)).map((skill) => ({ path, mode: state.state.settings.deploymentMode, skillId: skill.id, name: skill.name, contentHash: skill.contentHash })));
    target.pending = false;
  }
  return copy();
};
let nextGlobalID = 1;
const resolvePath = (root: string, path: string) => {
  const parts: string[] = [];
  for (const part of (path.startsWith("/") ? path : `${root}/${path}`).split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") parts.pop(); else parts.push(part);
  }
  return `/${parts.join("/")}`;
};
const templatePath = (path: string) => {
  path = path.trim();
  if (path.startsWith("~/")) return resolvePath("/test/home", path.slice(2));
  if (path.startsWith("/")) return resolvePath("/", path);
  const parts: string[] = [];
  for (const part of path.split("/")) {
    if (!part || part === ".") continue;
    if (part === ".." && parts.length && parts[parts.length - 1] !== "..") parts.pop(); else parts.push(part);
  }
  return parts.join("/") || ".";
};
const globalTargets = (keys: string[]) => state.globalApplications.filter((application) => keys.includes(application.key)).flatMap((application) => application.target ? [application.target] : []);
const localCandidates = [
  { name: "Local Writing", description: "Writing skill from an ordinary local directory", subpath: "writing", revision: "" },
  { name: "Local Review", description: "Review skill from the same parent directory", subpath: "nested/review", revision: "" },
];
let localPreviewID = "";
let localPreviewSequence = 0;
const previewLocal = async (path: string) => {
  const delay = Math.min(2000, Math.max(0, Number(testParameters.get("local-preview-delay")) || 0));
  if (delay) await new Promise((resolve) => setTimeout(resolve, delay));
  if (testParameters.has("local-preview-fail")) throw new Error("local skill directory is not a directory");
  localPreviewID = `local-preview-${++localPreviewSequence}`;
  document.documentElement.dataset.localPreviewPath = path;
  return { id: localPreviewID, local: true, source: path, ref: "", revision: "", candidates: testParameters.has("local-preview-empty") ? [] : localCandidates, warnings: [], skipped: [] };
};
const installLocal = async (id: string, paths: string[]) => {
  if (!localPreviewID || id !== localPreviewID) throw new Error("preview is no longer available; preview the source again");
  if (paths.some((path) => !localCandidates.some((candidate) => candidate.subpath === path))) throw new Error("skill was not offered by this preview");
  for (const item of localCandidates.filter((candidate) => paths.includes(candidate.subpath))) {
    const skill = { ...item, id: `local-import-${item.name}`, source: "", sourceId: "", contentHash: `local-${item.name}`, installedAt: "2026-10-12T00:00:00Z", updatedAt: "2026-10-12T00:00:00Z", upstreamRemoved: false, ref: "" };
    state.state.skills = [...state.state.skills.filter((existing) => existing.id !== skill.id), skill];
  }
  document.documentElement.dataset.localInstalledPaths = paths.join(",");
  localPreviewID = "";
  return copy();
};
Object.assign(api, {
  state: async () => copy(), setTheme: async () => {}, setLocale: async () => {},
  search: async (query: string) => state.state.skills.filter((skill) => skill.sourceId && `${skill.name} ${skill.source}`.toLowerCase().includes(query.trim().toLowerCase())).map((skill) => ({ name: skill.name, slug: `${skill.sourceId}/${skill.name}`, source: skill.source, installs: 100, url: skill.source, supported: true })),
  listSkills: async (page: number) => {
    const results = await api.search("");
    return { skills: results.slice(page * 200, (page + 1) * 200), total: results.length, hasMore: (page + 1) * 200 < results.length, page };
  },
  checkUpdates: async () => state.state.skills.filter((skill) => ["design", "code"].includes(skill.id)).map((skill) => ({
    skillId: skill.id, revision: `updated-${skill.id}`, summary: `Update ${skill.name}`,
    available: skill.revision !== `updated-${skill.id}`, removed: false, error: "",
  })),
  updateSkills, removeSkills,
  updateSkill: async (id: string) => updateSkills([id]),
  removeSkill: async (id: string) => removeSkills([id]),
  refreshSource: async () => copy(),
  syncSource: async (id: string, revision: string, paths: string[]) => {
    const source = state.state.sources.find((item) => item.id === id)!;
    if (revision !== source.revision) throw new Error("source revision changed");
    const selected = source.catalog.filter((item) => !item.skipReason && !item.removed && (source.mode === "all" || paths.includes(item.subpath)));
    if (paths.some((path) => !selected.some((item) => item.subpath === path))) throw new Error("skipped or unavailable skill selected");
    document.documentElement.dataset.syncedPaths = selected.map((item) => item.subpath).join(",");
    for (const item of selected) {
      const previous = state.state.skills.find((skill) => skill.sourceId === source.id && skill.subpath === item.subpath);
      if (previous) Object.assign(previous, { name: item.name, description: item.description, contentHash: item.contentHash, revision: item.revision });
      else state.state.skills.push({ ...item, id: item.subpath, source: source.repository, sourceId: source.id, ref: source.ref, installedAt: "2026-10-12T00:00:00Z", updatedAt: "2026-10-12T00:00:00Z", upstreamRemoved: false });
    }
    return copy();
  },
  preview: async () => ({
    id: "partial-preview", source: "owner/repo", ref: "main", revision: "1234567890abcdef",
    candidates: [{ name: "New Skill", description: "A valid new skill", subpath: "skills/new", revision: "1234567890abcdef" }],
    warnings: ['skipped repository entry "AGENTS.md": symlink is not supported'],
    skipped: [{ subpath: "skills/design", reason: 'unsupported skill content at "skills/design/AGENTS.md": symlink is not supported' }],
  }),
  previewLocal,
  install: installLocal,
  readSkillDocument: async () => "# 名称\n\n设置 must remain authored text.",
  saveSettings: async (settings: AppState["state"]["settings"]) => {
    if (settingsDelay) await new Promise((resolve) => setTimeout(resolve, settingsDelay));
    if (settingsFailure) { settingsFailure = false; throw new Error("Test settings save failed"); }
    const next = structuredClone(settings);
    if ((next.customProjectDirectories || []).some((directory) => !directory.name.trim() || !directory.path.trim())) throw new Error("custom project directory name and path are required");
    for (const directory of next.customProjectDirectories || []) {
      const path = directory.path.trim();
      const previous = state.state.settings.customProjectDirectories.find((item) => item.id === directory.id);
      if ((/^(?:[\\/]|~(?:[\\/]|$)|[A-Za-z]:)/.test(path) || /^(?:[\\/]|~(?:[\\/]|$)|[A-Za-z]:)/.test(templatePath(path))) && path !== previous?.path) throw new Error("custom project directory path must be relative to the project root");
    }
    next.customProjectDirectories = (next.customProjectDirectories || state.state.settings.customProjectDirectories).map((directory) => ({ ...directory, name: directory.name.trim(), path: templatePath(directory.path) }));
    if (next.customProjectDirectories.some((directory) => !directory.name || !directory.path)) throw new Error("custom project directory name and path are required");
    if (new Set(next.customProjectDirectories.map((directory) => directory.id)).size !== next.customProjectDirectories.length) throw new Error("duplicate custom project directory ID");
    if (new Set(next.customProjectDirectories.map((directory) => directory.path)).size !== next.customProjectDirectories.length) throw new Error("duplicate custom project directory path");
    const removed = new Set(state.state.settings.customProjectDirectories.filter((directory) => !next.customProjectDirectories.some((item) => item.id === directory.id)).map((directory) => directory.id));
    next.defaultProjectDirectories = next.defaultProjectDirectories.filter((id) => !removed.has(id));
    if (!next.defaultProjectDirectories.length) next.defaultProjectDirectories = ["agents"];
    const validIDs = new Set(["agents", "claude", "codex", "cursor", ...next.customProjectDirectories.map((directory) => directory.id)]);
    if (next.defaultProjectDirectories.some((id) => !validIDs.has(id))) throw new Error("unknown project directory");
    state.state.settings = next; return copy();
  },
  savePreset: async (preset: AppState["state"]["presets"][number]) => {
    const index = state.state.presets.findIndex((item) => item.id === preset.id);
    if (index >= 0) state.state.presets[index] = preset;
    else state.state.presets.push({ ...preset, id: "preset-test" });
    return copy();
  },
  saveProject: async (project: AppState["projects"][number]) => {
    const previous = state.projects.find((target) => target.id === project.id);
    const root = project.path.replace(/\/+$/, "");
    const defaults = state.state.settings.defaultProjectDirectories;
    const directories = project.directoryIds || defaults.filter((id) => ["agents", "claude", "codex", "cursor"].includes(id));
    const customPaths = project.customPaths || previous?.customPaths || state.state.settings.customProjectDirectories.filter((directory) => defaults.includes(directory.id)).map((directory) => directory.path);
    const paths = [...directories.map((id) => `${root}/.${id}/skills`), ...customPaths.map((path) => resolvePath(root, path))];
    if (!paths.length) throw new Error("select at least one project skill directory");
    if (new Set(paths).size !== paths.length) throw new Error("duplicate target paths");
    const destinationsChanged = !!previous && JSON.stringify([...(previous.paths || [previous.path])].sort()) !== JSON.stringify([...paths].sort());
    const target = { ...project, customPaths, id: project.id || "project-test", projectRoot: root, paths, path: paths[0], pending: project.pending || !!previous?.deployments.length && destinationsChanged };
    state.projects = [...state.projects.filter((item) => item.id !== target.id), target];
    state.state.targets = [...state.state.targets.filter((item) => item.id !== target.id), target];
    return copy();
  },
  saveGlobalDirectory: async (target: Target) => {
    const existing = state.globalApplications.find((item) => item.key === target.id && item.agentId === "custom");
    const path = resolvePath("/", target.path);
    if (!target.path.startsWith("/")) throw new Error("custom global directory must be absolute");
    if (existing?.target?.deployments.length && existing.path !== path) throw new Error("remove applied skills before changing the directory");
    if (state.globalApplications.some((item) => item.key !== target.id && (item.path === path || item.path.startsWith(`${path}/`) || path.startsWith(`${item.path}/`)))) throw new Error("target directories overlap");
    const saved = { ...target, id: existing?.key || `global-custom-${nextGlobalID++}`, path, paths: [path], pending: target.enabledSkillIds.length > 0 || target.enabledPresetIds.length > 0 };
    const application = { key: saved.id, agentId: "custom", agentName: "自定义", name: saved.name, path, description: "已保存的自定义全局技能目录。", target: saved };
    state.globalApplications = [...state.globalApplications.filter((item) => item.key !== saved.id), application];
    state.state.targets = [...state.state.targets.filter((item) => item.id !== saved.id), saved];
    return copy();
  },
  deleteGlobalDirectory: async (id: string) => {
    const existing = state.globalApplications.find((item) => item.key === id && item.agentId === "custom");
    if (!existing) throw new Error("custom global directory not found");
    if (existing.target?.deployments.length) throw new Error("remove applied skills before deleting the directory");
    state.globalApplications = state.globalApplications.filter((item) => item.key !== id);
    state.state.targets = state.state.targets.filter((item) => item.id !== id);
    state.state.settings.defaultGlobalDirectories = state.state.settings.defaultGlobalDirectories.filter((key) => key !== id);
    if (!state.state.settings.defaultGlobalDirectories.length) state.state.settings.defaultGlobalDirectories = ["codex-user"];
    return copy();
  },
  deleteProject: async (id: string) => {
    state.projects = state.projects.filter((item) => item.id !== id);
    state.state.targets = state.state.targets.filter((item) => item.id !== id);
    return copy();
  },
  setProjectSelection: async (id: string, skillIds: string[], presetIds: string[]) => {
    const target = state.projects.find((project) => project.id === id)!;
    if (!target) throw new Error("project not found");
    if (testParameters.has("project-selection-delay")) await new Promise((resolve) => setTimeout(resolve, Math.min(2000, Math.max(0, Number(testParameters.get("project-selection-delay")) || 300))));
    if (projectSelectionFailures > 0) { projectSelectionFailures--; throw new Error("Fixture selection save failed"); }
    document.documentElement.dataset.savedProjectID = id;
    document.documentElement.dataset.savedProjectSkills = skillIds.join(",");
    document.documentElement.dataset.savedProjectPresets = presetIds.join(",");
    document.documentElement.dataset.projectSelectionSaves = String(Number(document.documentElement.dataset.projectSelectionSaves || 0) + 1);
    target.enabledSkillIds = skillIds; target.enabledPresetIds = presetIds; target.pending = true; return copy();
  },
  setGlobalSelections: async (keys: string[], skillIds: string[], presetIds: string[]) => {
    if (testParameters.has("project-selection-delay")) await new Promise((resolve) => setTimeout(resolve, Math.min(2000, Math.max(0, Number(testParameters.get("project-selection-delay")) || 300))));
    document.documentElement.dataset.savedGlobalKeys = keys.join(",");
    document.documentElement.dataset.savedGlobalSkills = skillIds.join(",");
    document.documentElement.dataset.savedGlobalPresets = presetIds.join(",");
    for (const application of state.globalApplications.filter((item) => keys.includes(item.key))) {
      if (!application.target) {
        application.target = { id: application.key, name: application.agentName, kind: "global", path: application.path, paths: [application.path], enabledSkillIds: [], enabledPresetIds: [], deployments: [], pending: false };
        state.state.targets.push(application.target);
      }
      application.target.enabledSkillIds = skillIds; application.target.enabledPresetIds = presetIds; application.target.pending = true;
    }
    return copy();
  },
  planProject: async (id: string) => {
    if (testParameters.has("project-plan-delay")) await new Promise((resolve) => setTimeout(resolve, Math.min(2000, Math.max(0, Number(testParameters.get("project-plan-delay")) || 300))));
    document.documentElement.dataset.projectPlanID = id;
    return plan(state.projects.filter((target) => target.id === id));
  },
  planGlobals: async (keys: string[]) => plan(globalTargets(keys)),
  planGlobal: async (key: string) => plan(globalTargets([key])),
  applyProject: async (id: string) => {
    recordApplyCall("applyProject");
    const result = apply(state.projects.filter((target) => target.id === id));
    document.documentElement.dataset.projectAppliedID = id;
    return result;
  },
  applyGlobals: async (keys: string[]) => {
    recordApplyCall("applyGlobals");
    const result = apply(globalTargets(keys));
    document.documentElement.dataset.globalAppliedKeys = keys.join(",");
    return result;
  },
  applyGlobal: async (key: string) => {
    recordApplyCall("applyGlobal");
    const result = apply(globalTargets([key]));
    document.documentElement.dataset.globalAppliedKeys = key;
    return result;
  },
  forceApplyProject: async (id: string, reviewed: ApplyPlan) => {
    recordApplyCall("forceApplyProject");
    const result = apply(state.projects.filter((target) => target.id === id), true, reviewed);
    document.documentElement.dataset.projectAppliedID = id;
    return result;
  },
  forceApplyGlobals: async (keys: string[], reviewed: ApplyPlan) => {
    recordApplyCall("forceApplyGlobals");
    const result = apply(globalTargets(keys), true, reviewed);
    document.documentElement.dataset.globalAppliedKeys = keys.join(",");
    return result;
  },
  forceApplyGlobal: async (key: string, reviewed: ApplyPlan) => {
    recordApplyCall("forceApplyGlobal");
    const result = apply(globalTargets([key]), true, reviewed);
    document.documentElement.dataset.globalAppliedKeys = key;
    return result;
  },
  chooseDirectory: async () => testParameters.has("choose-directory-cancel") ? "" : testParameters.get("choose-directory") || "/test/project",
} satisfies Partial<typeof api>);
if (visualReview) {
  // Keep discovery, backup, and editing interactions inside the fixture as well.
  let nextVisualID = 1;
  const snapshotStates = new Map(state.snapshots.map((snapshot) => [snapshot.id, structuredClone(state.state)]));
  const availableSkills = state.state.sources[0].catalog.filter((skill) => !skill.removed && !skill.skipReason);
  let previewSource = "https://example.com/team/design";
  Object.assign(api, {
    search: async (query: string) => {
      const results = [
        { name: "Interface Design / 视觉对齐与界面设计", slug: "interface-design", source: "team/design", installs: 18420, url: "https://example.com/skills/interface-design", supported: true },
        { name: "Accessibility and Inclusive Interaction Patterns", slug: "accessibility", source: "team/design", installs: 9600, url: "https://example.com/skills/accessibility", supported: true },
        { name: "Design Tokens and Component Libraries", slug: "design-tokens", source: "team/design", installs: 3280, url: "https://example.com/skills/design-tokens", supported: true },
        { name: "Production-ready Code Review", slug: "code-review", source: "team/code", installs: 24600, url: "https://example.com/skills/code-review", supported: true },
      ];
      const requested = Number(testParameters.get("discovery-results"));
      const count = Math.max(results.length, Math.min(600, Number.isFinite(requested) ? Math.floor(requested) : results.length));
      for (let index = results.length; index < count; index++) {
        const number = String(index + 1).padStart(3, "0");
        results.push({ name: `Design workflow ${number} · 团队协作`, slug: `design-workflow-${number}`, source: "team/design", installs: 500 + index, url: `https://example.com/skills/design-workflow-${number}`, supported: true });
      }
      return results.filter((result) => `${result.name} ${result.slug} ${result.source}`.toLowerCase().includes(query.trim().toLowerCase()));
    },
    preview: async (source: string, ref: string) => {
      previewSource = source;
      return { id: "visual-preview", source, ref: ref || "main", revision: "9e8d7c6b5a40", candidates: availableSkills.map((skill) => ({ name: skill.name, description: skill.description, subpath: skill.subpath, revision: skill.revision })), warnings: [], skipped: [] };
    },
    install: async (id: string, paths: string[]) => {
      if (id.startsWith("local-preview-")) return installLocal(id, paths);
      for (const item of availableSkills.filter((skill) => paths.includes(skill.subpath))) {
        if (state.state.skills.some((skill) => skill.source === previewSource && skill.subpath === item.subpath)) continue;
        const source = state.state.sources.find((item) => item.repository === previewSource);
        state.state.skills.push({ ...item, id: `visual-installed-${nextVisualID++}`, source: previewSource, sourceId: source?.id || "", ref: "main", installedAt: "2026-10-12T02:45:00Z", updatedAt: "2026-10-12T02:45:00Z", upstreamRemoved: false });
      }
      return copy();
    },
    readSkillDocument: async (id: string) => {
      const skill = checkedSkills([id])[0];
      const delay = Math.min(2000, Math.max(0, Number(testParameters.get("document-delay")) || 0));
      if (delay) await new Promise((resolve) => setTimeout(resolve, delay));
      return `# ${skill.name}\n\n${skill.description}\n\n## 工作流程 / Workflow\n\n1. 明确用户目标，检查信息层级和阅读顺序。\n2. 对齐标题、表单、操作与内容边界。\n3. 验证长文本、中英文和紧凑窗口下的布局。\n\n## Quality checks\n\n- Consistent spacing and readable typography\n- Keyboard access and visible focus\n- Clear feedback before applying changes\n`;
    },
    saveSource: async (source: AppState["state"]["sources"][number]) => {
      const saved = { ...source, id: source.id || `visual-source-${nextVisualID++}` };
      state.state.sources = [...state.state.sources.filter((item) => item.id !== saved.id), saved];
      return copy();
    },
    deleteSource: async (id: string) => {
      state.state.sources = state.state.sources.filter((source) => source.id !== id);
      return copy();
    },
    relinkSourceSkill: async (skillId: string, sourceId: string, subpath: string) => {
      const skill = checkedSkills([skillId])[0];
      const source = state.state.sources.find((source) => source.id === sourceId);
      const item = source?.catalog.find((item) => item.subpath === subpath && !item.removed && !item.skipReason);
      if (!source || !item) throw new Error("source skill not found");
      Object.assign(skill, item, { sourceId, source: source.repository, ref: source.ref, upstreamRemoved: false });
      for (const target of state.state.targets) if (selectedSkills(target).has(skillId)) target.pending = true;
      return copy();
    },
    deletePreset: async (id: string) => {
      state.state.presets = state.state.presets.filter((preset) => preset.id !== id);
      for (const target of state.state.targets) {
        if (target.enabledPresetIds.includes(id)) target.pending = true;
        target.enabledPresetIds = target.enabledPresetIds.filter((presetId) => presetId !== id);
      }
      return copy();
    },
    setGlobalSelection: async (key: string, skillIds: string[], presetIds: string[]) => api.setGlobalSelections([key], skillIds, presetIds),
    createSnapshot: async () => {
      const id = `snapshot-visual-created-${nextVisualID++}`;
      snapshotStates.set(id, structuredClone(state.state));
      state.snapshots.unshift({ id, createdAt: "2026-10-12T02:45:00Z", reason: "", skillCount: state.state.skills.length, presetCount: state.state.presets.length });
      return copy();
    },
    restoreSnapshot: async (id: string) => {
      const saved = snapshotStates.get(id);
      if (!saved) throw new Error("snapshot not found");
      // Restore workspace content while keeping application configuration intact.
      state.state.skills = structuredClone(saved.skills);
      state.state.sources = structuredClone(saved.sources);
      state.state.presets = structuredClone(saved.presets);
      for (const target of state.state.targets) target.pending = true;
      return copy();
    },
    exportBackup: async (path: string) => path,
    importBackup: async () => copy(),
    chooseExportFile: async () => "/test/exports/racomi-workspace.zip",
    chooseImportFile: async () => "/test/imports/racomi-workspace.zip",
    defaultSkillsManagerPath: async () => "/test/home/Library/Application Support/skills-manager/skills.db",
    chooseSkillsManagerFile: async () => "/test/imports/skills-manager.db",
    previewSkillsManager: async (databasePath: string) => ({ id: "visual-skills-manager-preview", databasePath, skillCount: 3, presetCount: 1, sourceCount: 1, warnings: [], skillNames: ["Interface Design", "Code Review", "Team Writing"], presetNames: ["Team workflow"] }),
    importSkillsManager: async () => ({ app: copy(), result: { addedSkills: 0, skippedSkills: 3, addedPresets: 0, skippedPresets: 1, addedSources: 0, skippedSources: 1 } }),
  } satisfies Partial<typeof api>);
}
await import("../src/main");
