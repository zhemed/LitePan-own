<script setup lang="ts">
import "@/styles/admin-shared.css";
</script>

<template>
  <div class="input-action-field">
    <div class="input-action-field__main">
      <slot />
    </div>
    <slot name="action" />
  </div>
</template>
