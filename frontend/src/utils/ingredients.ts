// ingredientTokens splits an active-ingredient string such as
// "Paracetamol + Caffeine" into normalised single ingredients.
export function ingredientTokens(s: string | null | undefined): string[] {
  if (!s) return [];
  return s
    .split(/[,;+/]/)
    .map((t) => t.trim().toLowerCase())
    .filter(Boolean);
}

// sharedIngredient reports whether two ingredient strings have at least
// one ingredient in common.
export function sharedIngredient(
  a: string | null | undefined,
  b: string | null | undefined
): boolean {
  const left = new Set(ingredientTokens(a));
  return ingredientTokens(b).some((t) => left.has(t));
}
