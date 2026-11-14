import {
  DashboardService,
  PaymentMethodsService,
  SubscriptionsService,
  VendorsService,
  errorMessage,
} from "./api";
import type { DashboardSummary, PaymentMethod, Subscription, Vendor } from "./api";

export type StatusFilter = "all" | "active" | "trial" | "paused" | "cancelled";

export const store = $state({
  subscriptions: [] as Subscription[],
  paymentMethods: [] as PaymentMethod[],
  vendors: [] as Vendor[],
  summary: null as DashboardSummary | null,
  loading: false,
  error: "",
  search: "",
  statusFilter: "all" as StatusFilter,
});

async function attempt<T>(fn: () => Promise<T>): Promise<T | null> {
  try {
    store.error = "";
    return await fn();
  } catch (err) {
    store.error = errorMessage(err);
    return null;
  }
}

export async function loadSubscriptions(): Promise<void> {
  const rows = await attempt(() =>
    SubscriptionsService.List({
      statuses: store.statusFilter === "all" ? [] : [store.statusFilter],
      vendorId: "",
      paymentMethodId: "",
      search: store.search,
      limit: 0,
    }),
  );
  if (rows) store.subscriptions = rows ?? [];
}

export async function loadPaymentMethods(includeInactive = true): Promise<void> {
  const rows = await attempt(() => PaymentMethodsService.List(includeInactive));
  if (rows) store.paymentMethods = rows ?? [];
}

export async function loadVendors(): Promise<void> {
  const rows = await attempt(() => VendorsService.List());
  if (rows) store.vendors = rows ?? [];
}

export async function loadSummary(days = 30): Promise<void> {
  const summary = await attempt(() => DashboardService.Summary(days));
  if (summary) store.summary = summary;
}

export async function loadAll(): Promise<void> {
  store.loading = true;
  try {
    await Promise.all([loadSubscriptions(), loadPaymentMethods(), loadVendors(), loadSummary()]);
  } finally {
    store.loading = false;
  }
}

export function clearError(): void {
  store.error = "";
}
