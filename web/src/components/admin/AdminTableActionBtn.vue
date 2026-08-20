<script setup lang="ts">
type AdminTableActionIcon = "play" | "stop" | "edit" | "delete" | "log";

withDefaults(
  defineProps<{
    icon: AdminTableActionIcon;
    title: string;
    danger?: boolean;
    disabled?: boolean;
  }>(),
  { danger: false, disabled: false },
);

defineEmits<{ click: [MouseEvent] }>();
</script>

<template>
  <button
    type="button"
    class="admin-table__action-btn"
    :class="{ 'admin-table__action-btn--danger': danger }"
    :title="title"
    :disabled="disabled"
    @click="$emit('click', $event)"
  >
    <svg
      v-if="icon === 'play'"
      viewBox="0 0 16 16"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M4 2.5v11l9-5.5-9-5.5z" />
    </svg>
    <svg
      v-else-if="icon === 'stop'"
      viewBox="0 0 16 16"
      fill="currentColor"
      aria-hidden="true"
    >
      <rect x="4" y="4" width="8" height="8" rx="1" />
    </svg>
    <svg
      v-else-if="icon === 'edit'"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      aria-hidden="true"
    >
      <path d="M2.5 13.5h3l8-8-3-3-8 8v3z" />
      <path d="M9.5 3.5l3 3" />
    </svg>
    <svg
      v-else-if="icon === 'log'"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.4"
      aria-hidden="true"
    >
      <rect x="2" y="3" width="12" height="10" rx="1.5" />
      <path d="M5 6h6M5 8.5h4" stroke-linecap="round" />
    </svg>
    <svg
      v-else
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      aria-hidden="true"
    >
      <path d="M3 4h10" stroke-linecap="round" />
      <path d="M6 4V3.2c0-.7.5-1.2 1.2-1.2h1.6c.7 0 1.2.5 1.2 1.2V4" />
      <path d="M5 6v7c0 .6.4 1 1 1h4c.6 0 1-.4 1-1V6" />
    </svg>
  </button>
</template>
