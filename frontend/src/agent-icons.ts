import claude from "./assets/agents/claude.svg";
import codex from "./assets/agents/codex.svg";
import cursor from "./assets/agents/cursor.svg";
import { icon } from "./icons";

// Local assets follow Magpie's color-image / currentColor-mask treatment.
// Attribution and licenses are included in public/THIRD_PARTY_NOTICES.txt.
export function agentIcon(agentId: string): string {
  if (agentId === "claude") return `<img class="agent-icon" src="${claude}" alt="" aria-hidden="true" draggable="false">`;
  if (agentId === "codex") return `<img class="agent-icon" src="${codex}" alt="" aria-hidden="true" draggable="false">`;
  if (agentId === "cursor") return `<span class="agent-icon agent-icon-mask" style="--agent-icon: url('${cursor.replace(/'/g, "%27")}')" aria-hidden="true"></span>`;
  return icon(agentId === "agents" ? "layers" : "folder", "agent-icon");
}
