// Single entry point for the generated Wails bindings, so screens never import
// generated paths directly and the generated API can change in one place.
import {
  DashboardService,
  IngestionService,
  JobsService,
  PaymentMethodsService,
  ReviewService,
  SettingsService,
  SourcesService,
  SubscriptionsService,
  VendorsService,
} from "../../bindings/substrackt/internal/app";

export {
  DashboardService,
  IngestionService,
  JobsService,
  PaymentMethodsService,
  ReviewService,
  SettingsService,
  SourcesService,
  SubscriptionsService,
  VendorsService,
};
export type {
  BrowserConnector,
  BrowserLoginStatus,
  BrowserRuntime,
  BrowserRuntimeOption,
  DashboardSummary,
  Job,
  JobCounts,
  LLMTestResult,
  LLMUsage,
  ManualInput,
  Money,
  PaymentMethod,
  PaymentMethodInput,
  ReviewAcceptInput,
  ReviewBulkResult,
  ReviewFailure,
  ReviewItem,
  Settings,
  Source,
  SourceConfig,
  SourceInput,
  SourceTestResult,
  SpendBucket,
  Subscription,
  SubscriptionFilter,
  SubscriptionInput,
  Vendor,
  VendorInput,
} from "../../bindings/substrackt/internal/app";

/** Normalises errors thrown by Wails (usually plain strings) for display. */
export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return "Something went wrong";
}
