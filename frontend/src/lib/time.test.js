import { describe, expect, it } from "vitest";
import { inferGran, seriesFormatter, timeFormatter } from "@/lib/time";

// Local-time formatting: build expectations from the same local Date.
const t = new Date(2026, 9, 31, 14, 5).getTime(); // Oct 31 2026 14:05 local

describe("timeFormatter", () => {
  it("formats minute/hour buckets as HH:mm", () => {
    expect(timeFormatter("minute", 3600_000)(t)).toBe("14:05");
    expect(timeFormatter("hour", 6 * 3600_000)(t)).toBe("14:05");
  });
  it("adds the day for multi-day hourly ranges", () => {
    expect(timeFormatter("hour", 7 * 86_400_000)(t)).toBe("Oct 31 14:05");
  });
  it("formats day buckets as MMM dd", () => {
    expect(timeFormatter("day", 30 * 86_400_000)(t)).toBe("Oct 31");
  });
  it("tolerates junk", () => {
    expect(timeFormatter("hour")(undefined)).toBe("");
    expect(timeFormatter("hour")("nope")).toBe("");
  });
});

describe("seriesFormatter", () => {
  it("uses the payload granularity and window", () => {
    const f = seriesFormatter({ granularity: "hour", from: t - 86_400_000 * 7, to: t, points: [] });
    expect(f(t)).toBe("Oct 31 14:05");
  });
  it("infers granularity from bucket spacing", () => {
    expect(inferGran([{ t: 0 }, { t: 60_000 }])).toBe("minute");
    expect(inferGran([{ t: 0 }, { t: 3_600_000 }])).toBe("hour");
    expect(inferGran([{ t: 0 }, { t: 86_400_000 }])).toBe("day");
  });
  it("full mode always includes the date for sub-day buckets", () => {
    const f = seriesFormatter({ granularity: "minute", from: t - 3600_000, to: t }, { full: true });
    expect(f(t)).toBe("Oct 31 14:05");
  });
});
