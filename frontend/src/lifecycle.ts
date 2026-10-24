import { Events } from "@wailsio/runtime";
import { api } from "./api";
import { t } from "./i18n";
import { isDesktopRuntime } from "./desktop-runtime";

interface Options {
 flush: () => Promise<void>;
 busy: () => boolean;
 failed: () => boolean;
 retry: () => void;
 dirty: () => boolean;
 discard: () => void;
 error: (message: string) => void;
}
export function installLifecycle(options: Options): void {
 if (!isDesktopRuntime()) return;
 let active = 0;
 let cancelled = false;
 let dialogResolve: ((value:boolean)=>void) | undefined;
 const root = document.createElement("div"); root.id = "lifecycle-root"; document.body.append(root);
 const background = () => [document.querySelector<HTMLElement>("#app"), document.querySelector<HTMLElement>("#modal-root")].filter((el): el is HTMLElement => !!el);
 const lock = (on: boolean) => { document.body.dataset.lifecycleBlocked=String(on); for (const el of background()) el.inert = on || el.id === "app" && !!document.querySelector("#modal-root")?.children.length; };
 function dialog(title: string, description: string, choices: {label:string; value:boolean; primary?:boolean}[]): Promise<boolean> {
  root.innerHTML = `<div class="modal-backdrop"><section class="modal" role="dialog" aria-modal="true" tabindex="-1"><div class="modal-heading"><h2></h2></div><div class="modal-body"><p></p></div><div class="modal-footer"></div></section></div>`;
  root.querySelector("h2")!.textContent = title; root.querySelector("p")!.textContent = description;
  return new Promise(resolve => {
   dialogResolve=resolve;
   for (const choice of choices) { const button = document.createElement("button"); button.type="button"; button.className = `button ${choice.primary ? "primary" : ""}`; button.textContent=choice.label; button.onclick=()=>resolve(choice.value); root.querySelector(".modal-footer")!.append(button); }
   root.querySelector<HTMLButtonElement>("button")?.focus();
  });
 }
 const cancel = () => { cancelled = true; dialogResolve?.(false); dialogResolve=undefined; root.replaceChildren(); lock(false); };
 const unlisten = [
 Events.On("racomi:lifecycle-resumed", () => { lock(false); }),
 Events.On("racomi:lifecycle-cancelled", event => { if (event.data === active) cancel(); }),
 Events.On("racomi:lifecycle-error", event => { cancel(); options.error(String(event.data)); }),
 Events.On("racomi:lifecycle", async event => {
  const request = event.data as {id:number; page:number; action:string};
  if (active) return;
  active=request.id; cancelled=false; lock(true);
  const previousFocus = document.activeElement;
  let allow=false;
  let discardApproved=false;
  try {
   if (options.dirty()) {
    const discard = await dialog(t("Discard unsaved edits?"), t("The current form has not been submitted. Continuing will discard these edits."), [{label:t("Continue editing"),value:false},{label:t("Discard edits and continue"),value:true}]);
    if (!discard || cancelled) return;
    discardApproved=true;
   }
   // This overlay leaves the underlying form and all of its input intact.
   root.innerHTML = `<div class="modal-backdrop"><section class="modal" role="dialog" aria-modal="true"><div class="modal-heading"><h2></h2></div><div class="modal-body"><p></p></div><div class="modal-footer"><button type="button" class="button"></button></div></section></div>`;
   root.querySelector("h2")!.textContent=t("Saving changes…"); root.querySelector("p")!.textContent=t("The requested action continues after saving. Your edits are kept.");
   const cancelButton=root.querySelector<HTMLButtonElement>("button")!; cancelButton.textContent=t("Cancel"); cancelButton.onclick=()=>{ cancel(); void api.completeLifecycle(request.id,request.page,false); }; cancelButton.focus();
   const deadline=Date.now()+20000;
   while (options.busy()) { if (cancelled) return; if (Date.now()>deadline) throw Error(t("Wait for the current operation to finish, then try again.")); await new Promise(resolve=>setTimeout(resolve,50)); }
   try { await options.flush(); } catch (error) { if (!options.failed()) throw error; }
   if (options.failed()) {
    const retry = await dialog(t("Changes have not been saved"), t("Your edits are kept. Retry saving before continuing."), [{label:t("Continue editing"),value:false},{label:t("Retry saving"),value:true,primary:true}]);
    if (!retry || cancelled) return;
    options.retry(); await options.flush();
    if (options.failed()) throw Error(t("Your edits are kept. Retry saving before continuing."));
   }
   allow=!cancelled;
  } catch (error) { options.error(error instanceof Error ? error.message : String(error)); }
  finally {
   const accepted = await api.completeLifecycle(request.id,request.page,allow).catch(()=>false);
   if(!accepted) allow=false;
   if(allow && discardApproved) options.discard();
   root.replaceChildren(); active=0; if(!allow) lock(false);
   if (!allow && previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus();
  }
 })];
 root.addEventListener("keydown", event => {
  if(event.key === "Escape") { event.preventDefault(); root.querySelector<HTMLButtonElement>("button")?.click(); }
  if(event.key === "Tab") { const buttons=[...root.querySelectorAll<HTMLButtonElement>("button")]; const index=buttons.indexOf(document.activeElement as HTMLButtonElement); event.preventDefault(); buttons[(index+(event.shiftKey?-1:1)+buttons.length)%buttons.length]?.focus(); }
  event.stopPropagation();
 });
 window.addEventListener("pagehide",()=>{ for(const off of unlisten) off(); },{once:true});
 void api.frontendReady().catch(()=>{});
}
