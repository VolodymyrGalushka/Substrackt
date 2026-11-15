export type Route = "dashboard" | "subscriptions" | "review" | "activity" | "payment-methods" | "settings";

const ROUTES: Route[] = [
  "dashboard",
  "subscriptions",
  "review",
  "activity",
  "payment-methods",
  "settings",
];

function current(): Route {
  if (typeof window === "undefined") return "dashboard";
  const raw = window.location.hash.replace(/^#\/?/, "");
  return (ROUTES as string[]).includes(raw) ? (raw as Route) : "dashboard";
}

export const nav = $state({ route: current() });

export function go(route: Route): void {
  window.location.hash = `/${route}`;
}

if (typeof window !== "undefined") {
  window.addEventListener("hashchange", () => {
    nav.route = current();
  });
}
