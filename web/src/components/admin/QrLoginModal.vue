<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { qrPoll, qrStart, type QrStatus } from "@/api/qr";
import type { FieldOption } from "@/api/types";
import { toast } from "@/composables/useToast";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";

const props = withDefaults(
  defineProps<{
    open: boolean;
    driverType: string;
    config?: string;
    deviceOptions?: FieldOption[];
    deviceField?: string;
  }>(),
  { config: "", deviceOptions: () => [], deviceField: "" },
);
const emit = defineEmits<{ close: []; success: [credentials: Record<string, string>] }>();

type Phase = "loading" | "waiting" | "success" | "failed" | "expired" | "error";

const phase = ref<Phase>("loading");
const qrImage = ref("");
const message = ref("");
const token = ref("");
const expiresIn = ref(0);
const panelTitle = ref("扫码登录");
const hintText = ref("请使用对应网盘 App扫码，成功后授权信息将填入表单");
const device = ref("");

let pollTimer: ReturnType<typeof setTimeout> | null = null;
let countdownTimer: ReturnType<typeof setInterval> | null = null;
let errorStreak = 0;

const showDevicePicker = computed(() => props.deviceOptions.length > 0);

const expireText = computed(() => {
  if (phase.value === "expired") return "二维码已过期";
  if (expiresIn.value <= 0) return "";
  return `二维码剩余有效时间： ${expiresIn.value} 秒`;
});

function clearPoll() {
  if (pollTimer) {
    clearTimeout(pollTimer);
    pollTimer = null;
  }
}

function clearCountdown() {
  if (countdownTimer) {
    clearInterval(countdownTimer);
    countdownTimer = null;
  }
}

function startCountdown(seconds: number) {
  clearCountdown();
  expiresIn.value = Math.max(0, seconds || 0);
  if (expiresIn.value <= 0) return;
  countdownTimer = setInterval(() => {
    expiresIn.value = Math.max(0, expiresIn.value - 1);
    if (expiresIn.value <= 0 && phase.value === "waiting") {
      phase.value = "expired";
      message.value = "二维码已过期，请重新获取";
      clearPoll();
      clearCountdown();
    }
  }, 1000);
}

function scheduleNextPoll(delay = 2000) {
  clearPoll();
  pollTimer = setTimeout(() => void poll(), delay);
}

function defaultDevice() {
  return (
    props.deviceOptions.find((o) => o.value === "web")?.value ??
    props.deviceOptions[0]?.value ??
    ""
  );
}

function buildStartConfig(): string {
  let cfg: Record<string, unknown> = {};
  if (props.config) {
    try {
      cfg = JSON.parse(props.config) as Record<string, unknown>;
    } catch {
      cfg = {};
    }
  }
  if (props.deviceField && device.value) {
    cfg[props.deviceField] = device.value;
  }
  return JSON.stringify(cfg);
}

function onDeviceChange(e: Event) {
  const next = (e.target as HTMLSelectElement).value;
  if (!next || next === device.value) return;
  device.value = next;
  if (props.open) void start();
}

async function start() {
  clearPoll();
  phase.value = "loading";
  qrImage.value = "";
  message.value = "";
  token.value = "";
  expiresIn.value = 0;
  panelTitle.value = "扫码登录";
  hintText.value = "请使用对应网盘 App扫码，成功后授权信息将填入表单";
  errorStreak = 0;
  try {
    const res = await qrStart(props.driverType, buildStartConfig());
    if (!res.success || !res.data?.token || !res.data.qr_image_base64) {
      throw new Error(res.message || "获取二维码失败");
    }
    qrImage.value = res.data.qr_image_base64;
    token.value = res.data.token;
    if (res.data.title?.trim()) panelTitle.value = res.data.title.trim();
    if (res.data.hint?.trim()) hintText.value = res.data.hint.trim();
    phase.value = "waiting";
    startCountdown(res.data.expires_in || 300);
    scheduleNextPoll(2000);
  } catch (e) {
    phase.value = "error";
    message.value = e instanceof Error ? e.message : "获取二维码失败";
  }
}

const phaseByStatus: Record<QrStatus, Phase> = {
  waiting: "waiting",
  success: "success",
  failed: "failed",
  expired: "expired",
};

async function poll() {
  if (!token.value) return;
  try {
    const res = await qrPoll(props.driverType, token.value);
    if (!res.success || !res.data?.status) {
      throw new Error(res.message || "轮询失败");
    }
    errorStreak = 0;
    const { status, cookie, access_token: accessToken, refresh_token: refreshToken, message: msg } = res.data;
    const credentials: Record<string, string> = {};
    if (cookie) credentials.cookie = cookie;
    if (accessToken) credentials.access_token = accessToken;
    if (refreshToken) credentials.refresh_token = refreshToken;
    if (status === "success" && Object.keys(credentials).length > 0) {
      phase.value = "success";
      clearPoll();
      clearCountdown();
      toast.success("扫码登录成功，授权信息已自动填入表单");
      emit("success", credentials);
      return;
    }
    if (status === "success") {
      phase.value = "failed";
      message.value = msg || "扫码成功但未获取到授权信息，请重试";
      clearPoll();
      clearCountdown();
      return;
    }
    if (status === "failed" || status === "expired") {
      phase.value = phaseByStatus[status];
      message.value = msg || (status === "expired" ? "二维码已过期" : "扫码登录失败");
      clearPoll();
      clearCountdown();
      return;
    }
    scheduleNextPoll(2000);
  } catch {
    errorStreak += 1;
    if (errorStreak >= 5) {
      phase.value = "error";
      message.value = "网络异常，请重试";
      clearPoll();
      clearCountdown();
      return;
    }
    scheduleNextPoll(3000);
  }
}

function handleClose() {
  clearPoll();
  clearCountdown();
  emit("close");
}

watch(
  () => props.open,
  (open) => {
    if (open && props.driverType) {
      device.value = defaultDevice();
      void start();
    } else {
      clearPoll();
      clearCountdown();
    }
  },
);

onUnmounted(() => {
  clearPoll();
  clearCountdown();
});
</script>

<template>
  <AppModal :open="open" bare @close="handleClose">
    <div class="qr-panel">
      <div class="qr-panel-header">
        <div class="qr-panel-title">
          <i class="fas fa-qrcode"></i>
          <span>{{ panelTitle }}</span>
        </div>
        <button class="qr-panel-close" type="button" aria-label="关闭" @click="handleClose">×</button>
      </div>

      <div v-if="showDevicePicker" class="qr-device-bar">
        <span class="qr-device-label">设备来源</span>
        <select class="qr-device-select" :value="device" @change="onDeviceChange">
          <option v-for="o in deviceOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </div>

      <div class="qr-panel-body">
        <div v-if="phase === 'loading'" class="qr-state qr-state--loading">
          <BusySpinner :size="28" color="var(--brand)" />
          <span>正在生成二维码...</span>
        </div>

        <div v-else-if="phase === 'waiting' || phase === 'success'" class="qr-state qr-state--waiting">
          <img class="qr-image" :src="qrImage" alt="扫码二维码" />
          <div class="qr-hint">{{ hintText }}</div>
          <div v-if="phase === 'waiting' && expireText" class="qr-countdown">{{ expireText }}</div>
          <div v-else class="qr-success">已获取授权信息</div>
        </div>

        <div v-else class="qr-state qr-state--failed">
          <i class="fas fa-circle-exclamation"></i>
          <div class="qr-result-title">{{ phase === "expired" ? "二维码已过期" : "扫码登录失败" }}</div>
          <div class="qr-hint">{{ message || "二维码已失效，请关闭后重新获取" }}</div>
          <button class="qr-retry" type="button" @click="start">重新获取</button>
        </div>
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.qr-panel {
  width: 360px;
  max-width: 92vw;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border-radius: 16px;
  background: var(--surface);
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.2);
}

.qr-panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 20px;
  background: var(--panel-head-bg);
  border-bottom: 1px solid var(--border);
  border-top-left-radius: 16px;
  border-top-right-radius: 16px;
}

.qr-panel-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--text);
  font-size: 15px;
  font-weight: 600;
}

.qr-panel-title i {
  color: var(--brand);
  font-size: 15px;
}

.qr-device-bar {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 10px 20px;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--surface) 60%, transparent);
}

.qr-device-label {
  color: var(--text-muted);
  font-size: 13px;
}

.qr-device-select {
  height: 30px;
  padding: 0 28px 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm, 8px);
  background: var(--surface);
  color: var(--text);
  font-size: 13px;
  cursor: pointer;
}

.qr-panel-close {
  width: 28px;
  height: 28px;
  border: 0;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 24px;
  line-height: 1;
}

.qr-panel-close:hover {
  color: var(--text);
}

.qr-panel-body {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 320px;
  padding: 24px 20px;
  box-sizing: border-box;
}

.qr-image {
  width: 220px;
  height: 220px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: #fff;
  padding: 8px;
  box-sizing: border-box;
}

.qr-state {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  text-align: center;
  color: var(--text);
  font-size: 13px;
}

.qr-state--failed {
  color: #b91c1c;
}

.qr-state i {
  font-size: 40px;
}

.qr-state--loading i {
  color: var(--brand);
}

.qr-hint {
  margin-top: 0;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.5;
  text-align: center;
}

.qr-countdown {
  margin-top: 0;
  color: var(--text-muted);
  font-size: 12px;
  text-align: center;
}

.qr-success {
  color: #16a34a;
  font-size: 13px;
  font-weight: 600;
}

.qr-result-title {
  color: var(--text);
  font-size: 15px;
  font-weight: 600;
}

.qr-retry {
  margin-top: 10px;
  height: 34px;
  padding: 0 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-muted);
  cursor: pointer;
}

.qr-retry:hover {
  color: var(--brand);
  border-color: color-mix(in srgb, var(--brand) 40%, var(--border));
}
</style>
