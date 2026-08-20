import { toast } from "@/composables/useToast";
import { confirmUploadConflict } from "@/composables/confirmUpload";
import { filesApi } from "@/api/files";
import { getSystemUploadJunkReason, normalizeUploadRelativePath } from "@/composables/upload/uploadTaskFormatters";
import type { UploadActionsCtx } from "@/composables/upload/useUploadPanelActions";
import type { UploadTask } from "@/types/upload";

export function useUploadFolderPlanner(ctx: UploadActionsCtx) {
  const { deps, store, stream, dispatcher } = ctx;

  async function listRemoteEntries(parentId: string, forceRefresh = false) {
    const res = await filesApi.list(deps.selectedAccountId.value as number, parentId, { forceRefresh });
    return res.items;
  }

  async function ensureRemoteFolder(parentId: string, folderName: string) {
    try {
      const res = await filesApi.createFolder({
        account_id: deps.selectedAccountId.value as number,
        parent_id: parentId,
        name: folderName,
      });
      return res.folder_id;
    } catch {
      await new Promise((r) => setTimeout(r, 800));
      const entries = await listRemoteEntries(parentId, true);
      const hit = entries.find((e) => e.is_dir && e.name === folderName);
      if (hit) return hit.id;
      throw new Error(`创建文件夹 "${folderName}" 失败`);
    }
  }

  async function handleUploadFolderChange(event: Event) {
    const target = event.target as HTMLInputElement;
    const selectedFiles = Array.from(target.files || []);
    if (!selectedFiles.length) return;
    try {
      store.uploadTaskPanelOpen.value = true;
      store.uploadTaskPanelLoading.value = true;
      store.uploadTaskPanelLoadingText.value = "正在准备上传文件夹任务...";
      await stream.refreshUploadTaskServerConcurrency();

      const normalized = selectedFiles
        .map((file) => ({ file, relativePath: normalizeUploadRelativePath(file) }))
        .filter((x) => x.relativePath);
      if (!normalized.length) throw new Error("未读取到可上传的文件，空文件夹暂不支持");

      const roots = [...new Set(normalized.map((x) => x.relativePath.split("/")[0]).filter(Boolean))];
      if (roots.length !== 1) throw new Error("当前仅支持一次上传一个文件夹");

      const folderIdMap = new Map<string, string>([["", deps.currentPath.value]]);
      const skipped: UploadTask[] = [];
      const uploadable: typeof normalized = [];

      for (const item of normalized) {
        const reason = getSystemUploadJunkReason(item.relativePath);
        if (reason) {
          skipped.push(store.createSkippedUploadTask(item.file, reason, { file_name: item.relativePath }));
          continue;
        }
        uploadable.push(item);
      }
      skipped.forEach(store.addLocalUploadTask);

      const dirs = new Set<string>();
      for (const item of uploadable) {
        const parts = item.relativePath.split("/");
        parts.pop();
        const acc: string[] = [];
        for (const p of parts) {
          acc.push(p);
          dirs.add(acc.join("/"));
        }
      }
      const sortedDirs = [...dirs].sort((a, b) => a.split("/").length - b.split("/").length);
      for (const rel of sortedDirs) {
        const parts = rel.split("/");
        const name = parts[parts.length - 1];
        const parentRel = parts.slice(0, -1).join("/");
        const parentId = folderIdMap.get(parentRel);
        if (parentId === undefined) throw new Error(`未找到上级目录：${parentRel || "根目录"}`);
        const folderId = await ensureRemoteFolder(parentId, name);
        folderIdMap.set(rel, folderId);
        if (parentId === deps.currentPath.value) {
          store.markFolderUploadRefreshPending();
        }
      }

      let batchConflictPolicy: string | null = null;
      const nameCache = new Map<string, Set<string>>();
      const plans: {
        file: File;
        conflictPolicy: string;
        localTask: UploadTask;
        targetPath: string;
        displayName: string;
        targetDisplayPath: string;
      }[] = [];

      for (const item of uploadable) {
        const parts = item.relativePath.split("/");
        parts.pop();
        const relDir = parts.join("/");
        const targetPath = folderIdMap.get(relDir) || deps.currentPath.value;
        const targetDisplayPath = dispatcher.buildUploadTargetDisplayPath(relDir);
        if (!nameCache.has(targetPath)) {
          const entries = await listRemoteEntries(targetPath);
          nameCache.set(targetPath, new Set(entries.map((e) => e.name.toLowerCase())));
        }
        let conflictPolicy = "overwrite";
        if (nameCache.get(targetPath)!.has(item.file.name.toLowerCase())) {
          if (!batchConflictPolicy) {
            const r = await confirmUploadConflict(item.relativePath);
            if (!r) break;
            if (r.checked) batchConflictPolicy = r.action;
            conflictPolicy = r.action;
          } else conflictPolicy = batchConflictPolicy;
          if (conflictPolicy === "skip") {
            store.addLocalUploadTask(
              store.createSkippedUploadTask(item.file, "检测到同名文件，已跳过", {
                file_name: item.relativePath,
                target_path: targetPath,
                target_display_path: targetDisplayPath,
              }),
            );
            continue;
          }
        }
        const localTask = store.createLocalUploadTask(item.file, {
          file_name: item.relativePath,
          target_path: targetPath,
          target_display_path: targetDisplayPath,
        });
        plans.push({
          file: item.file,
          conflictPolicy,
          localTask,
          targetPath,
          displayName: item.relativePath,
          targetDisplayPath,
        });
      }

      for (const p of plans) {
        store.localUploadTaskPayloads.set(p.localTask.task_id, {
          file: p.file,
          conflictPolicy: p.conflictPolicy,
          targetPath: p.targetPath,
          displayName: p.displayName,
          targetDisplayPath: p.targetDisplayPath,
        });
        store.ensureUploadTaskDisplayOrder(p.localTask);
      }
      if (plans.length) {
        store.localUploadTasks.value = [...plans.map((p) => p.localTask), ...store.localUploadTasks.value];
        void dispatcher.startUploadTaskScheduler();
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "准备上传文件夹失败");
    } finally {
      store.uploadTaskPanelLoading.value = false;
      store.uploadTaskPanelLoadingText.value = "正在准备上传任务...";
      target.value = "";
    }
  }

  return { handleUploadFolderChange };
}
