import {
  DashboardService,
  JobsService,
  ReviewService,
  SettingsService,
  PaymentMethodsService,
  SourcesService,
  SubscriptionsService,
  VendorsService,
  errorMessage,
} from "./api";
import type {
  DashboardSummary,
  Job,
  JobCounts,
  LLMUsage,
  PaymentMethod,
  ReviewItem,
  Settings,
  Source,
  Subscription,
  Vendor,
} from "./api";

export type StatusFilter = "all" | "active" | "trial" | "paused" | "cancelled";
export type ReviewStatus = "pending" | "accepted" | "rejected" | "superseded";

export const store = $state({
  subscriptions: [] as Subscription[],
  paymentMethods: [] as PaymentMethod[],
  vendors: [] as Vendor[],
  summary: null as DashboardSummary | null,
  reviewItems: [] as ReviewItem[],
  reviewStatus: "pending" as ReviewStatus,
  reviewCount: 0,
  jobs: [] as Job[],
  jobCounts: null as JobCounts | null,
  settings: null as Settings | null,
  sources: [] as Source[],
  llmUsage: [] as LLMUsage[],
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

export async function loadReview(status: ReviewStatus = store.reviewStatus): Promise<void> {
  const rows = await attempt(() => ReviewService.List(status));
  if (rows) store.reviewItems = rows ?? [];
}

export async function loadReviewCount(): Promise<void> {
  const count = await attempt(() => ReviewService.Count());
  if (count != null) store.reviewCount = count;
}

export async function loadJobs(limit = 100): Promise<void> {
  const [rows, counts] = await Promise.all([
    attempt(() => JobsService.List(limit)),
    attempt(() => JobsService.Counts()),
  ]);
  if (rows) store.jobs = rows ?? [];
  if (counts) store.jobCounts = counts;
}

export async function loadSettings(): Promise<void> {
  const settings = await attempt(() => SettingsService.Get());
  if (settings) store.settings = settings;
}

export async function loadSources(): Promise<void> {
  const rows = await attempt(() => SourcesService.List());
  if (rows) store.sources = rows ?? [];
}

export async function loadLLMUsage(limit = 25): Promise<void> {
  const rows = await attempt(() => SettingsService.Usage(limit));
  if (rows) store.llmUsage = rows ?? [];
}

export async function loadAll(): Promise<void> {
  store.loading = true;
  try {
    await Promise.all([
      loadSubscriptions(),
      loadPaymentMethods(),
      loadVendors(),
      loadSummary(),
      loadReview(),
      loadReviewCount(),
      loadJobs(),
      loadSettings(),
      loadSources(),
      loadLLMUsage(),
    ]);
  } finally {
    store.loading = false;
  }
}

export function clearError(): void {
  store.error = "";
}
