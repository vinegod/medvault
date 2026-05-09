// Single source of truth for cross-page tunables. Avoids the previous
// situation where DEFAULT_LOW_STOCK was redeclared in four files and
// drifting was a silent risk.

export const DEFAULT_LOW_STOCK = 3;
