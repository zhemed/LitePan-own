import { computed, toValue, type MaybeRefOrGetter } from "vue";

export function formatAccountPathLabel(accountName: string, path: string): string {
  const normalized = (path || "/").trim() || "/";
  return `${accountName}·${normalized}`;
}

export function resolveAccountName(
  accountId: number,
  accounts: Array<{ id: number; name?: string }>,
): string {
  if (!accountId) return "";
  return accounts.find((a) => a.id === accountId)?.name ?? `#${accountId}`;
}

export function useAccountPathLabel(options: {
  accountId: MaybeRefOrGetter<number>;
  path: MaybeRefOrGetter<string>;
  accounts: MaybeRefOrGetter<Array<{ id: number; name?: string }>>;
  placeholder?: string;
  ready?: MaybeRefOrGetter<boolean>;
}) {
  const placeholder = options.placeholder ?? "点击浏览选择账号及目录";
  const display = computed(() => {
    const accountId = toValue(options.accountId);
    if (!accountId) return "";
    if (options.ready !== undefined && !toValue(options.ready)) return "";
    const name = resolveAccountName(accountId, toValue(options.accounts));
    return formatAccountPathLabel(name, toValue(options.path));
  });
  const title = computed(() => display.value || placeholder);
  return { display, title, placeholder };
}
