<script setup lang="ts">
import "@/styles/settings-panel.css";

const model = defineModel<boolean>({ default: false });

withDefaults(
  defineProps<{
    label: string;
    offLabel?: string;
    onLabel?: string;
  }>(),
  { offLabel: "关闭", onLabel: "开启" },
);
</script>

<template>
  <div class="settings-segment" role="radiogroup" :aria-label="label">
    <button
      type="button"
      class="settings-segment__btn"
      :class="{ 'settings-segment__btn--active': model }"
      role="radio"
      :aria-checked="model"
      @click="model = true"
    >
      {{ onLabel }}
    </button>
    <button
      type="button"
      class="settings-segment__btn"
      :class="{ 'settings-segment__btn--active': !model }"
      role="radio"
      :aria-checked="!model"
      @click="model = false"
    >
      {{ offLabel }}
    </button>
  </div>
</template>
