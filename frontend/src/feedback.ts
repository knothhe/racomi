let dismissTimer: number | undefined;
let observing = false;

function positionFeedback() {
  const root = document.querySelector<HTMLElement>("#toast-root");
  if (!root?.firstElementChild) return;
  const lifted = !!document.querySelector("#modal-root > *");
  root.classList.toggle("feedback-lift", lifted);
  const metadata = document.querySelector(".statusbar-meta")?.getBoundingClientRect();
  root.style.maxWidth = lifted ? "calc(100vw - 32px)" : `${Math.max(120, (metadata?.left ?? innerWidth) - 30)}px`;
}

function observeLayout() {
  if (observing) return;
  observing = true;
  const observer = new MutationObserver(positionFeedback);
  for (const id of ["app", "modal-root"]) {
    const element = document.getElementById(id);
    if (element) observer.observe(element, { childList: true });
  }
  window.addEventListener("resize", positionFeedback);
}

export function showFeedback(message: string, error = false) {
  const root = document.querySelector<HTMLElement>("#toast-root");
  if (!root) return;
  observeLayout();
  window.clearTimeout(dismissTimer);
  const feedback = document.createElement("div");
  feedback.className = `feedback ${error ? "feedback-error" : "feedback-success"}`;
  feedback.textContent = message;
  feedback.title = message;
  feedback.setAttribute("role", error ? "alert" : "status");
  root.replaceChildren(feedback);
  document.body.classList.add("has-feedback");
  positionFeedback();
  dismissTimer = window.setTimeout(() => {
    root.replaceChildren();
    root.classList.remove("feedback-lift");
    document.body.classList.remove("has-feedback");
  }, error ? 8000 : 3500);
}
