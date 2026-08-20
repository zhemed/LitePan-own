<script setup lang="ts">
import SvgIcon from "@/components/icons/SvgIcon.vue";
import {
  APP_URL,
  APP_VERSION_BADGE,
  GITHUB_URL,
} from "@/version";

const badges = [
  {
    key: "docs",
    href: APP_URL,
    icon: "globe",
    label: "当前版本",
    value: APP_VERSION_BADGE,
  },
  {
    key: "github",
    href: GITHUB_URL,
    icon: "github",
    label: "项目地址",
    value: "Github 仓库",
  },
] as const;
</script>

<template>
  <footer class="footer">
    <div class="container footer__inner">
      <template v-for="(item, index) in badges" :key="item.key">
        <span v-if="index > 0" class="footer__sep" aria-hidden="true">|</span>
        <a
          class="footer-item"
          :href="item.href"
          target="_blank"
          rel="noopener noreferrer"
        >
          <SvgIcon :name="item.icon" :size="14" class-name="footer-item__icon" />
          <span class="footer-item__label">{{ item.label }}</span>
          <span class="footer-item__value">{{ item.value }}</span>
        </a>
      </template>
    </div>
  </footer>
</template>

<style scoped>
.footer {
  margin-top: 16px;
  background: var(--surface-sunken);
  border-top: 1px solid var(--border);
  width: 100%;
}
.footer__inner {
  min-height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 14px;
  padding: 12px 0;
}
.footer__sep {
  color: var(--border);
  font-size: 13px;
  line-height: 1;
  user-select: none;
}

.footer-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  text-decoration: none;
  color: inherit;
  font-size: 13px;
  line-height: 1.2;
  white-space: nowrap;
}
.footer-item__icon {
  color: var(--text-muted);
}
.footer-item__label {
  color: var(--text-muted);
}
.footer-item__label::after {
  content: ":";
}
.footer-item__value {
  color: var(--text-regular);
  font-weight: 500;
}

@media (max-width: 640px) {
  .footer__inner {
    gap: 10px;
  }
  .footer-item {
    font-size: 12px;
  }
}
</style>
