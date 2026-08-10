import { useEffect, useState } from "react";
import { api } from "@/lib/api";

// One /system/stats fetch per page load is plenty for static facts like node name/shard count.
let cached = null;
let inflight = null;

/** Normalize the bits of /system/stats that the UI uses as labels. */
export function systemInfoFrom(stats) {
  if (!stats) return { nodeName: null, shardCount: null };
  const shardCount =
    typeof stats.shard_count === "number"
      ? stats.shard_count
      : Array.isArray(stats.shards) && stats.shards.length
        ? stats.shards.length
        : null;
  return { nodeName: stats.node_name || stats.node || null, shardCount };
}

export function loadSystemInfo() {
  if (cached) return Promise.resolve(cached);
  if (!inflight) {
    inflight = api
      .get("/system/stats")
      .then(({ data }) => {
        cached = systemInfoFrom(data);
        return cached;
      })
      .catch(() => systemInfoFrom(null))
      .finally(() => {
        inflight = null;
      });
  }
  return inflight;
}

/** Seed the cache from a full stats payload a page already fetched. */
export function primeSystemInfo(stats) {
  if (stats) cached = systemInfoFrom(stats);
}

export function resetSystemInfo() {
  cached = null;
  inflight = null;
}

/** { nodeName, shardCount } — both null until known (or if the server doesn't report them). */
export function useSystemInfo() {
  const [info, setInfo] = useState(() => cached || systemInfoFrom(null));
  useEffect(() => {
    let alive = true;
    loadSystemInfo().then((i) => alive && setInfo(i));
    return () => {
      alive = false;
    };
  }, []);
  return info;
}

/** "4 Postgres shards" / "the Postgres shards" when the count is unknown. */
export const shardsPhrase = (n, noun = "Postgres shards") => (n ? `${n} ${noun}` : `the ${noun}`);

/** links_s0..s3 style range label. */
export const shardRange = (n) => (n ? `links_s0..s${n - 1}` : "links_s0..sN");
