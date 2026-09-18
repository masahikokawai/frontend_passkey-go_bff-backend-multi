// backend-js/src/flags.jsの型付き移植。ロジックは変更していない。

import type { Pool } from 'mysql2/promise';

interface FeatureFlagRow {
  enabled: number;
  default_variation: string | null;
  variations: string | null;
}

export async function fetchBool(pool: Pool, flagKey: string): Promise<boolean | null> {
  const [rows] = await pool.execute<any[]>(
    'SELECT enabled, default_variation, variations FROM feature_flags WHERE flag_key = ?',
    [flagKey],
  );
  if (rows.length === 0) return null;
  const row = rows[0] as FeatureFlagRow;
  const enabled = !!row.enabled;
  if (!enabled) return false;

  const defaultVariation = row.default_variation || '';
  let variations: Record<string, unknown>;
  try {
    variations = row.variations ? JSON.parse(row.variations) : { on: true, off: false };
  } catch {
    variations = { on: true, off: false };
  }
  const v = variations[defaultVariation];
  return typeof v === 'boolean' ? v : false;
}

export class FlagCache {
  flagKey: string;
  value: boolean;
  private timer: NodeJS.Timeout | null = null;

  constructor(flagKey: string) {
    this.flagKey = flagKey;
    this.value = false;
  }

  static async spawn(pool: Pool, flagKey: string, pollIntervalMs: number): Promise<FlagCache> {
    const cache = new FlagCache(flagKey);
    const initial = await fetchBool(pool, flagKey);
    if (initial !== null) cache.value = initial;

    cache.timer = setInterval(() => {
      fetchBool(pool, flagKey)
        .then((v) => {
          if (v !== null) cache.value = v;
        })
        .catch(() => {
          // ポーリング失敗時は直前の値を保持する(backend-rust/backend-jsと同じ、フェイルセーフ)
        });
    }, pollIntervalMs);
    cache.timer.unref?.();

    return cache;
  }

  get(): boolean {
    return this.value;
  }
}
