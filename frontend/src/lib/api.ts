// Single entry point for the generated Wails bindings, so screens never import
// generated paths directly and the generated API can change in one place.
import {
  DashboardService,
  PaymentMethodsService,
  SubscriptionsService,
  VendorsService,
} from "../../bindings/substrackt/internal/app";

export { DashboardService, PaymentMethodsService, SubscriptionsService, VendorsService };
export type {
  DashboardSummary,
  Money,
  PaymentMethod,
  PaymentMethodInput,
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
