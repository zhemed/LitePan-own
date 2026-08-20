import {
  formatRelayPart,
  formatRelaySpeed,
  formatUploadPart,
  getUploadTaskDriverBadge,
  getUploadTaskDisplayStatus,
  getUploadTaskPhaseLabel,
  getUploadTaskSpeedText,
  getUploadTaskStatusText,
  isUploadTaskActive,
  shouldShowUploadTaskHairline,
  shouldShowUploadTaskMetaPercent,
} from "@/composables/upload/uploadTaskFormatters";
import { useLocalUploadDispatcher } from "@/composables/upload/useLocalUploadDispatcher";
import { useUploadTaskActions } from "@/composables/upload/useUploadTaskActions";
import { useUploadTaskStore } from "@/composables/upload/useUploadTaskStore";
import { useUploadTaskStream } from "@/composables/upload/useUploadTaskStream";
import type { UploadRuntimeHooks, UploadTaskDeps } from "@/composables/upload/uploadTaskTypes";

export type { UploadTaskDeps as Deps } from "@/composables/upload/uploadTaskTypes";

export function useUploadTasks(deps: UploadTaskDeps) {
  const store = useUploadTaskStore(deps);
  store.restoreLocalUploadTasks();

  const hooks: UploadRuntimeHooks = {
    startScheduler: async () => {},
    fetchTasks: async () => {},
    startPolling: () => {},
    stopPolling: () => {},
    connectStream: () => {},
    disconnectStream: () => {},
    closePanel: () => {},
  };

  const stream = useUploadTaskStream(deps, store, hooks);
  const dispatcher = useLocalUploadDispatcher(deps, store, stream);
  const actions = useUploadTaskActions(deps, store, stream, dispatcher);

  hooks.startScheduler = dispatcher.startUploadTaskScheduler;
  hooks.fetchTasks = stream.fetchUploadTasks;
  hooks.startPolling = stream.startUploadTaskPolling;
  hooks.stopPolling = stream.stopUploadTaskPolling;
  hooks.connectStream = stream.connectUploadTaskStream;
  hooks.disconnectStream = stream.disconnectUploadTaskStream;
  hooks.closePanel = actions.closeUploadTaskPanel;

  const getUploadTaskPhaseLabelBound = (task: Parameters<typeof getUploadTaskPhaseLabel>[0]) =>
    getUploadTaskPhaseLabel(task, store.pendingRemoteResumeTaskIds, store.localDispatchingTaskIds);

  const getUploadTaskDisplayStatusBound = (task: Parameters<typeof getUploadTaskDisplayStatus>[0]) =>
    getUploadTaskDisplayStatus(task, store.pendingRemoteResumeTaskIds);

  const getUploadTaskDriverBadgeBound = (
    task: Parameters<typeof getUploadTaskDriverBadge>[0],
  ) => getUploadTaskDriverBadge(task, deps.accounts.value);

  const getRelayTaskDriverBadge = (task: {
    source_driver_type?: string;
    source_account_id?: number;
    source_account_name?: string;
  }) =>
    getUploadTaskDriverBadge(
      {
        driver_type: task.source_driver_type,
        account_id: task.source_account_id ?? 0,
        account_name: task.source_account_name,
      },
      deps.accounts.value,
    );

  return {
    uploadTaskPanelOpen: store.uploadTaskPanelOpen,
    taskPanelCategory: store.taskPanelCategory,
    uploadTaskPanelLoading: store.uploadTaskPanelLoading,
    uploadTaskPanelLoadingText: store.uploadTaskPanelLoadingText,
    uploadTaskServerConcurrency: store.uploadTaskServerConcurrency,
    displayUploadTasks: store.displayUploadTasks,
    activeUploadTasks: store.activeUploadTasks,
    uploadTaskLabel: store.uploadTaskLabel,
    getUploadTaskStatusText,
    formatUploadPart,
    getUploadTaskSpeedText,
    getUploadTaskDriverBadge: getUploadTaskDriverBadgeBound,
    getUploadTaskDisplayStatus: getUploadTaskDisplayStatusBound,
    isUploadTaskActive,
    getUploadTaskPhaseLabel: getUploadTaskPhaseLabelBound,
    shouldShowUploadTaskMetaPercent,
    shouldShowUploadTaskHairline,
    handleDeleteUploadTask: actions.handleDeleteUploadTask,
    handleDeleteUploadTasks: actions.handleDeleteUploadTasks,
    handleUploadTaskPrimaryAction: actions.handleUploadTaskPrimaryAction,
    openUploadTaskPanel: actions.openUploadTaskPanel,
    closeUploadTaskPanel: actions.closeUploadTaskPanel,
    openUploadNoticeFromPanel: actions.openUploadNoticeFromPanel,
    handleUploadFile: actions.handleUploadFile,
    handleUploadFolder: actions.handleUploadFolder,
    handleUploadFileChange: actions.handleUploadFileChange,
    handleUploadFolderChange: actions.handleUploadFolderChange,
    fetchUploadTasks: stream.fetchUploadTasks,
    refreshUploadTaskServerConcurrency: stream.refreshUploadTaskServerConcurrency,
    getRelayTaskDriverBadge,
    handleDeleteRelayTasks: actions.handleDeleteRelayTasks,
    formatRelaySpeed,
    formatRelayPart,
    activeRelayTasks: store.activeRelayTasks,
    failedRelayTasks: store.failedRelayTasks,
    activeRelayCount: store.activeRelayCount,
    relayTasks: store.relayTasks,
    cleanupUploadTasks: stream.cleanupUploadTasks,
  };
}
