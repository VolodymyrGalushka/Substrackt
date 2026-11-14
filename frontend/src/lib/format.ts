import type { Money } from "./api";

/** Formats a Money value using the browser's locale. */
export function formatMoney(m: Money | null | undefined, opts: { compact?: boolean } = {}): string {
  if (!m) return "—";
  const divisor = 10 ** m.scale;
  const value = m.amountMinor / divisor;
  try {
    return new Intl.NumberFormat(undefined, {
      style: "currency",
      currency: m.currency,
      minimumFractionDigits: m.scale,
      maximumFractionDigits: m.scale,
      notation: opts.compact ? "compact" : "standard",
    }).format(value);
  } catch {
    return `${value.toFixed(m.scale)} ${m.currency}`;
  }
}

/** Formats a "YYYY-MM-DD" date for display. */
export function formatDate(date: string | null | undefined): string {
  if (!date) return "—";
  const d = new Date(`${date}T00:00:00`);
  if (Number.isNaN(d.getTime())) return date;
  return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

/** Human-friendly distance to a "YYYY-MM-DD" date. */
export function relativeToToday(date: string | null | undefined): string {
  if (!date) return "";
  const target = new Date(`${date}T00:00:00`);
  if (Number.isNaN(target.getTime())) return "";
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const days = Math.round((target.getTime() - today.getTime()) / 86_400_000);
  if (days === 0) return "today";
  if (days === 1) return "tomorrow";
  if (days === -1) return "yesterday";
  if (days > 1 && days < 31) return `in ${days} days`;
  if (days > 30 && days < 365) return `in ${Math.round(days / 30)} months`;
  if (days < -1 && days > -31) return `${Math.abs(days)} days ago`;
  if (days >= 365) return `in ${(days / 365).toFixed(1)} years`;
  return `${Math.abs(days)} days ago`;
}

/** Renders a billing interval, e.g. "every 3 months". */
export function cycleLabel(count: number, unit: string): string {
  if (count === 1) {
    switch (unit) {
      case "day":
        return "daily";
      case "week":
        return "weekly";
      case "month":
        return "monthly";
      case "year":
        return "yearly";
    }
  }
  return `every ${count} ${unit}s`;
}

const CURRENCY_SCALES: Record<string, number> = {
  JPY: 0,
  KRW: 0,
  BTC: 8,
  ETH: 8,
  SOL: 9,
  USDC: 6,
  USDT: 6,
  DOGE: 8,
};

/** Default decimal scale for a currency code. */
export function scaleFor(currency: string): number {
  return CURRENCY_SCALES[currency.toUpperCase()] ?? 2;
}

/** Parses a major-unit amount string into minor units. */
export function toMinorUnits(input: string, scale: number): number {
  const normalized = input.replace(/\s/g, "").replace(",", ".");
  const value = Number.parseFloat(normalized);
  if (!Number.isFinite(value)) return 0;
  return Math.round(value * 10 ** scale);
}

/** Converts minor units to a major-unit input string. */
export function toMajorUnits(minor: number, scale: number): string {
  if (!minor) return "";
  return (minor / 10 ** scale).toFixed(scale).replace(/\.?0+$/, "");
}

export function todayISO(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function statusLabel(status: string): string {
  switch (status) {
    case "trial":
      return "Trial";
    case "active":
      return "Active";
    case "paused":
      return "Paused";
    case "cancelled":
      return "Cancelled";
    case "expired":
      return "Expired";
    default:
      return status;
  }
}

export function paymentTypeLabel(type: string): string {
  switch (type) {
    case "card":
      return "Card";
    case "bank_account":
      return "Bank account";
    case "paypal":
      return "PayPal";
    case "apple_pay":
      return "Apple Pay";
    case "google_pay":
      return "Google Pay";
    case "crypto":
      return "Crypto";
    case "store_credit":
      return "Store credit";
    default:
      return "Other";
  }
}
