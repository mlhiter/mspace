import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Cloud, Clock3, FileUp, Globe2, HardDrive, Network, Settings2, Trash2, X } from "lucide-react";
import {
  api,
  queryKeys,
  type Cluster,
  type ClusterInput,
  type KubeconfigDiscoveryResult,
  type KubeconfigImportResult,
} from "@mspace/core";
import { t as translate, useMspaceTranslation } from "@mspace/i18n";
import {
  Button,
  CollectionEmptyState,
  Field,
  InlineMeta,
  Input,
  Notice,
  PageFrame,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  StatusBadge,
  cn,
} from "@mspace/ui";
import { RelativeTime } from "./time";

const DEFAULT_KUBE_IMPORT_PROMPT_KEY = "mspace.clusters.defaultKubeImportPrompted";

const emptyClusterForm: ClusterInput = {
  name: "",
  kubeconfigPath: "",
  kubeContext: "",
  imageRegistryPrefix: "",
  exposureMode: "nodeport",
  nodeHost: "",
  previewDomain: "",
  ingressClass: "",
  status: "configured",
};

function clusterToForm(cluster: Cluster): ClusterInput {
  return {
    name: cluster.name,
    kubeconfigPath: cluster.kubeconfigPath,
    kubeContext: cluster.kubeContext,
    imageRegistryPrefix: cluster.imageRegistryPrefix,
    exposureMode: cluster.exposureMode === "ingress" ? "ingress" : "nodeport",
    nodeHost: cluster.nodeHost,
    previewDomain: cluster.previewDomain,
    ingressClass: cluster.ingressClass,
    status: cluster.status || "configured",
  };
}

export function ClustersPage() {
  const { t } = useMspaceTranslation();
  const queryClient = useQueryClient();
  const clustersQuery = useQuery({
    queryKey: queryKeys.clusters,
    queryFn: api.listClusters,
  });
  const [settingsCluster, setSettingsCluster] = useState<Cluster | null>(null);
  const [settingsForm, setSettingsForm] = useState<ClusterInput>(emptyClusterForm);
  const [defaultImportOpen, setDefaultImportOpen] = useState(false);
  const [defaultDiscoveryRequested, setDefaultDiscoveryRequested] = useState(false);
  const [defaultDiscovery, setDefaultDiscovery] = useState<KubeconfigDiscoveryResult | null>(null);
  const [selectedDefaultPaths, setSelectedDefaultPaths] = useState<string[]>([]);
  const [importSummary, setImportSummary] = useState("");

  const importKubeconfigs = useMutation({
    mutationFn: api.importKubeconfigFiles,
    onSuccess: async () => {
      setImportSummary("");
      await queryClient.invalidateQueries({ queryKey: queryKeys.clusters });
    },
  });
  const discoverDefaultKubeconfigs = useMutation({
    mutationFn: api.discoverDefaultKubeconfigs,
    onSuccess: async (result) => {
      setDefaultDiscovery(result);
      setSelectedDefaultPaths(result.candidates.map((candidate) => candidate.path));
      setDefaultImportOpen(true);
    },
  });
  const updateCluster = useMutation({
    mutationFn: (input: ClusterInput) => {
      if (!settingsCluster) throw new Error(t("clusters.noClusterSelected"));
      return api.updateCluster(settingsCluster.id, input);
    },
    onSuccess: async () => {
      setSettingsCluster(null);
      setSettingsForm(emptyClusterForm);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.clusters }),
      ]);
    },
  });
  const deleteCluster = useMutation({
    mutationFn: api.deleteCluster,
    onSuccess: async () => {
      setSettingsCluster(null);
      setSettingsForm(emptyClusterForm);
      await queryClient.invalidateQueries({ queryKey: queryKeys.clusters });
    },
  });

  const clusters = clustersQuery.data || [];
  const canSave = canSubmitCluster(settingsForm);
  const settingsClusterInUse = Boolean(
    settingsCluster && (settingsCluster.projectCount > 0 || settingsCluster.environmentCount > 0),
  );

  function openSettings(cluster: Cluster) {
    setSettingsCluster(cluster);
    setSettingsForm(clusterToForm(cluster));
    updateCluster.reset();
    deleteCluster.reset();
  }

  function submitSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settingsCluster || !canSave) return;
    updateCluster.mutate(normalizeClusterForm(settingsForm));
  }

  async function importFromPicker() {
    setImportSummary("");
    importKubeconfigs.reset();
    if (!window.mspaceDesktop?.selectKubeconfigFiles) {
      setImportSummary(t("clusters.pickerDesktopOnly"));
      return;
    }
    const paths = await window.mspaceDesktop.selectKubeconfigFiles();
    if (paths.length === 0) return;
    importKubeconfigs.mutate(paths, {
      onSuccess: async (result) => {
        setImportSummary(formatImportResult(result));
        await queryClient.invalidateQueries({ queryKey: queryKeys.clusters });
      },
    });
  }

  useEffect(() => {
    if (clustersQuery.isPending || clusters.length > 0) return;
    if (window.localStorage.getItem(DEFAULT_KUBE_IMPORT_PROMPT_KEY) === "1") return;
    if (defaultDiscoveryRequested || discoverDefaultKubeconfigs.isPending) return;
    setDefaultDiscoveryRequested(true);
    discoverDefaultKubeconfigs.mutate();
  }, [clusters.length, clustersQuery.isPending, defaultDiscoveryRequested, discoverDefaultKubeconfigs]);

  function closeDefaultImportPrompt() {
    window.localStorage.setItem(DEFAULT_KUBE_IMPORT_PROMPT_KEY, "1");
    setDefaultImportOpen(false);
  }

  return (
    <PageFrame
      title={t("clusters.title")}
      subtitle={t("clusters.subtitle")}
      actions={
        <Button variant="secondary" onClick={importFromPicker} disabled={importKubeconfigs.isPending}>
          <FileUp data-icon />
          {importKubeconfigs.isPending ? t("clusters.importing") : t("clusters.importKubeconfig")}
        </Button>
      }
    >
      {importSummary ? <Notice>{importSummary}</Notice> : null}
      {importKubeconfigs.error ? <Notice tone="danger">{importKubeconfigs.error.message}</Notice> : null}
      {discoverDefaultKubeconfigs.error ? <Notice tone="danger">{discoverDefaultKubeconfigs.error.message}</Notice> : null}

      {clustersQuery.isPending ? (
        <div className="mt-4 rounded-[10px] bg-[color:var(--surface)] px-4 py-6 text-[13px] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
          {t("clusters.loading")}
        </div>
      ) : clusters.length === 0 ? (
        <div className="mt-4">
          <CollectionEmptyState
            icon={Cloud}
            title={t("clusters.emptyTitle")}
            body={t("clusters.emptyBody")}
            action={
              <Button variant="secondary" onClick={importFromPicker} disabled={importKubeconfigs.isPending}>
                <FileUp data-icon />
                {t("clusters.importKubeconfig")}
              </Button>
            }
          />
        </div>
      ) : (
        <div className="mt-4 rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="grid grid-cols-[minmax(180px,1fr)_minmax(220px,1.4fr)_150px_120px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
            <span>{t("clusters.cluster")}</span>
            <span>{t("clusters.access")}</span>
            <span>{t("clusters.usage")}</span>
            <span className="text-right">{t("clusters.actions")}</span>
          </div>
          <div className="divide-y divide-[color:var(--line)]">
            {clusters.map((cluster) => (
              <ClusterRow key={cluster.id} cluster={cluster} onSettings={() => openSettings(cluster)} />
            ))}
          </div>
        </div>
      )}

      {settingsCluster ? (
        <ClusterModal
          title={t("clusters.settingsTitle")}
          description={t("clusters.settingsDescription")}
          value={settingsForm}
          error={updateCluster.error || deleteCluster.error}
          isPending={updateCluster.isPending}
          canSubmit={canSave}
          onChange={setSettingsForm}
          onClose={() => setSettingsCluster(null)}
          onSubmit={submitSettings}
          footerStart={
            <Button
              type="button"
              variant="danger"
              disabled={settingsClusterInUse || deleteCluster.isPending || updateCluster.isPending}
              title={settingsClusterInUse ? t("clusters.inUseDeleteTitle") : undefined}
              onClick={() => deleteCluster.mutate(settingsCluster.id)}
            >
              <Trash2 data-icon />
              {deleteCluster.isPending ? t("clusters.deleting") : t("clusters.deleteCluster")}
            </Button>
          }
        />
      ) : null}

      {defaultImportOpen ? (
        <DefaultKubeImportPrompt
          discovery={defaultDiscovery}
          selectedPaths={selectedDefaultPaths}
          isPending={importKubeconfigs.isPending}
          onToggle={(path) => {
            setSelectedDefaultPaths((paths) =>
              paths.includes(path) ? paths.filter((item) => item !== path) : [...paths, path],
            );
          }}
          onImport={() => {
            closeDefaultImportPrompt();
            importKubeconfigs.mutate(selectedDefaultPaths, {
              onSuccess: async (result) => {
                setImportSummary(formatImportResult(result));
                await queryClient.invalidateQueries({ queryKey: queryKeys.clusters });
              },
            });
          }}
          onSkip={closeDefaultImportPrompt}
        />
      ) : null}
    </PageFrame>
  );
}

function ClusterRow(props: { cluster: Cluster; onSettings: () => void }) {
  const { cluster } = props;
  const { t } = useMspaceTranslation();
  const exposure = cluster.exposureMode === "ingress" ? "ingress" : "nodeport";
  return (
    <article className="grid grid-cols-[minmax(180px,1fr)_minmax(220px,1.4fr)_150px_120px] items-center gap-4 px-4 py-3 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <h3 className="truncate text-[15px] font-semibold leading-6">{cluster.name}</h3>
          <StatusBadge value={cluster.status || "configured"} />
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          <InlineMeta icon={Clock3}><RelativeTime value={cluster.updatedAt} /></InlineMeta>
          <InlineMeta icon={Network}>{exposure}</InlineMeta>
        </div>
      </div>

      <div className="min-w-0">
        <div className="flex items-center gap-2 text-[13px] font-medium leading-5 text-[color:var(--muted-strong)]">
          <HardDrive data-icon />
          <span className="truncate">{cluster.kubeconfigPath}</span>
        </div>
        <div className="mt-1 flex items-center gap-2 text-[12px] leading-5 text-[color:var(--muted)]">
          <Globe2 data-icon className="shrink-0" />
          <span className="truncate">{cluster.imageRegistryPrefix}</span>
        </div>
      </div>

      <div className="text-[13px] leading-5 text-[color:var(--muted)]">
        <div>{t("clusters.projects", { count: cluster.projectCount })}</div>
        <div>{t("clusters.envs", { count: cluster.environmentCount })}</div>
      </div>

      <div className="flex justify-end">
        <Button variant="secondary" size="sm" onClick={props.onSettings}>
          <Settings2 data-icon />
          {t("clusters.settings")}
        </Button>
      </div>
    </article>
  );
}

function DefaultKubeImportPrompt(props: {
  discovery: KubeconfigDiscoveryResult | null;
  selectedPaths: string[];
  isPending: boolean;
  onToggle: (path: string) => void;
  onImport: () => void;
  onSkip: () => void;
}) {
  const { t } = useMspaceTranslation();
  const candidates = props.discovery?.candidates || [];
  const skipped = props.discovery?.skipped || [];
  const canImport = props.selectedPaths.length > 0 && !props.isPending;

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onSkip();
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props.onSkip]);

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label={t("clusters.closeDefaultImportBackdrop")} className="absolute inset-0 cursor-default" onClick={props.onSkip} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="default-kube-import-title"
        className="relative max-h-[calc(100vh-40px)] w-full max-w-[620px] overflow-auto rounded-[12px] bg-[color:var(--paper)] p-5 shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2 id="default-kube-import-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">
              {t("clusters.importPromptTitle")}
            </h2>
            <p className="mt-1 max-w-[58ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
              {t("clusters.importPromptPrefix")}{" "}
              <code className="rounded-[4px] bg-[color:var(--block)] px-1 py-0.5 font-mono text-[12px] text-[color:var(--muted-strong)]">~/.kube</code>.
              {" "}
              {t("clusters.importPromptSuffix")}
            </p>
          </div>
          <button
            type="button"
            aria-label={t("clusters.closeModal")}
            className="grid size-9 shrink-0 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95"
            onClick={props.onSkip}
          >
            <X data-icon />
          </button>
        </div>
        {candidates.length > 0 ? (
          <div className="mb-4 grid gap-2">
            {candidates.map((candidate) => (
              <label
                key={candidate.path}
                className="flex cursor-pointer items-start gap-3 rounded-[9px] bg-[color:var(--block)] px-3 py-2.5 shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color] hover:bg-[color:var(--hover)]"
              >
                <input
                  type="checkbox"
                  checked={props.selectedPaths.includes(candidate.path)}
                  onChange={() => props.onToggle(candidate.path)}
                  className="mt-1 size-4 accent-[color:var(--ink)]"
                />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-mono text-[12px] leading-5 text-[color:var(--muted-strong)]">
                    {candidate.path}
                  </span>
                  <span className="mt-1 block text-[12px] leading-5 text-[color:var(--muted)]">
                    {t("clusters.contextsSummary", {
                      count: candidate.contexts.length,
                      suffix: candidate.contexts.length === 1 ? "" : "s",
                      contexts: candidate.contexts.slice(0, 3).join(", "),
                    })}
                    {candidate.contexts.length > 3 ? ` ${t("clusters.moreContexts", { count: candidate.contexts.length - 3 })}` : ""}
                  </span>
                </span>
              </label>
            ))}
          </div>
        ) : (
          <div className="mb-4 rounded-[9px] bg-[color:var(--block)] px-3 py-3 text-[13px] leading-6 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
            {t("clusters.noKubeconfigContextsPrefix")}{" "}
            <code className="rounded-[4px] bg-[color:var(--surface)] px-1 py-0.5 font-mono text-[12px] text-[color:var(--muted-strong)]">~/.kube</code>.
          </div>
        )}
        {skipped.length > 0 ? (
          <div className="mb-4 rounded-[9px] bg-[color:var(--surface)] px-3 py-2.5 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
            {t("clusters.skippedFiles", { count: skipped.length, suffix: skipped.length === 1 ? "" : "s", reason: skipped[0]?.reason })}
          </div>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={props.onSkip} disabled={props.isPending}>
            {t("clusters.skip")}
          </Button>
          <Button type="button" onClick={props.onImport} disabled={!canImport}>
            <FileUp data-icon />
            {props.isPending ? t("clusters.importing") : t("clusters.importSelected", { count: props.selectedPaths.length || "" })}
          </Button>
        </div>
      </section>
    </div>
  );
}

function ClusterModal(props: {
  title: string;
  description: string;
  value: ClusterInput;
  error?: Error | null;
  isPending: boolean;
  canSubmit: boolean;
  footerStart?: ReactNode;
  onChange: (value: ClusterInput) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}) {
  const { t } = useMspaceTranslation();

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onClose();
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props.onClose]);

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label={t("clusters.closeClusterDialogBackdrop")} className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="cluster-modal-title"
        className="relative max-h-[calc(100vh-40px)] w-full max-w-[660px] overflow-auto rounded-[12px] bg-[color:var(--paper)] p-5 shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2 id="cluster-modal-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">{props.title}</h2>
            <p className="mt-1 max-w-[58ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
              {props.description}
            </p>
          </div>
          <button
            type="button"
            aria-label={t("clusters.closeModal")}
            className="grid size-9 shrink-0 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95"
            onClick={props.onClose}
          >
            <X data-icon />
          </button>
        </div>

        <form className="grid gap-4" onSubmit={props.onSubmit}>
          {props.error ? <Notice tone="danger">{props.error.message}</Notice> : null}
          <Field label={t("clusters.clusterName")}>
            <Input
              value={props.value.name}
              onChange={(event) => props.onChange({ ...props.value, name: event.target.value })}
              placeholder="test-cluster"
            />
          </Field>
          <div className="grid gap-3 md:grid-cols-2">
            <Field label={t("clusters.kubeconfigPath")}>
              <Input
                readOnly
                value={props.value.kubeconfigPath}
                placeholder="/Users/mlhiter/.kube/test"
                className="cursor-default text-[color:var(--muted-strong)]"
              />
            </Field>
            <Field label={t("clusters.kubeContext")}>
              <Input
                value={props.value.kubeContext}
                onChange={(event) => props.onChange({ ...props.value, kubeContext: event.target.value })}
                placeholder={t("clusters.optional")}
              />
            </Field>
          </div>
          <Field label={t("clusters.imageRegistryPrefix")}>
            <Input
              value={props.value.imageRegistryPrefix}
              onChange={(event) => props.onChange({ ...props.value, imageRegistryPrefix: event.target.value })}
              placeholder="registry.example.com/team/project"
            />
          </Field>
          <div className="grid gap-3 md:grid-cols-2">
            <Field label={t("clusters.defaultExposure")}>
              <Select
                value={props.value.exposureMode}
                onValueChange={(value) => props.onChange({ ...props.value, exposureMode: value as ClusterInput["exposureMode"] })}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="nodeport">NodePort</SelectItem>
                  <SelectItem value="ingress">Ingress</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label={t("clusters.nodeHost")}>
              <Input
                value={props.value.nodeHost}
                onChange={(event) => props.onChange({ ...props.value, nodeHost: event.target.value })}
                placeholder={t("clusters.optional")}
              />
            </Field>
            <Field label={t("clusters.previewDomain")} hint={t("clusters.previewDomainHint")}>
              <Input
                value={props.value.previewDomain}
                onChange={(event) => props.onChange({ ...props.value, previewDomain: event.target.value })}
                placeholder="preview.example.com"
              />
            </Field>
            <Field label={t("clusters.ingressClass")}>
              <Input
                value={props.value.ingressClass}
                onChange={(event) => props.onChange({ ...props.value, ingressClass: event.target.value })}
                placeholder={t("clusters.optional")}
              />
            </Field>
          </div>
          <div className={cn("mt-1 flex flex-wrap gap-2", props.footerStart ? "justify-between" : "justify-end")}>
            {props.footerStart ? <div>{props.footerStart}</div> : null}
            <div className="flex gap-2">
              <Button type="button" variant="secondary" onClick={props.onClose} disabled={props.isPending}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={!props.canSubmit || props.isPending}>
                {props.isPending ? t("clusters.saving") : t("clusters.saveCluster")}
              </Button>
            </div>
          </div>
        </form>
      </section>
    </div>
  );
}

function normalizeClusterForm(form: ClusterInput): ClusterInput {
  return {
    ...form,
    name: form.name.trim(),
    kubeconfigPath: form.kubeconfigPath.trim(),
    kubeContext: form.kubeContext.trim(),
    imageRegistryPrefix: form.imageRegistryPrefix.trim(),
    nodeHost: form.nodeHost.trim(),
    previewDomain: form.previewDomain.trim(),
    ingressClass: form.ingressClass.trim(),
    status: form.status || "configured",
  };
}

function canSubmitCluster(form: ClusterInput) {
  const normalized = normalizeClusterForm(form);
  if (!normalized.name || !normalized.kubeconfigPath || !normalized.imageRegistryPrefix) return false;
  if (normalized.exposureMode === "ingress" && !normalized.previewDomain) return false;
  return true;
}

function formatImportResult(result: KubeconfigImportResult) {
  const imported = result.imported.length;
  const skipped = result.skipped.length;
  if (imported === 0 && skipped === 0) {
    return translate("clusters.noneFound");
  }
  if (skipped === 0) {
    return translate("clusters.importedClusters", { count: imported, suffix: imported === 1 ? "" : "s" });
  }
  const firstReason = result.skipped[0]?.reason;
  return translate("clusters.importedClustersWithSkipped", {
    imported,
    importedSuffix: imported === 1 ? "" : "s",
    skipped,
    reason: firstReason ? ` (${firstReason})` : "",
  });
}
