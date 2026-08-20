<script setup lang="ts">
import "@/styles/admin-shared.css";

defineProps<{
  icon?: string;
  title?: string;
  description?: string;
}>();
</script>

<template>
  <div class="admin-empty-state">
    <span v-if="icon" class="admin-empty-state__icon">{{ icon }}</span>
    <p v-if="title" class="admin-empty-state__title">{{ title }}</p>
    <p v-if="description" class="admin-empty-state__desc">{{ description }}</p>
    <slot />
  </div>
</template>
