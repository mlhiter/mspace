import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Cloud, Clock3, FileUp, Globe2, HardDrive, Network, Server, Settings2, Trash2, X } from "lucide-react";
import {
  controlPlaneApi,
  queryKeys,
  type Cluster,
  type ClusterInput,
  type Environment,
  type EnvironmentInput,
  type KubeconfigDiscoveryResult,
  type KubeconfigImportResult,
} from "@mspace/core";
import { t as translate, useMspaceTranslation } from "@mspace/i18n";
import {
  Button,
  CollectionEmptyState,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
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
  useMspaceToast,
} from "@mspace/ui";
import { RelativeTime } from "./time";
import { useMspaceAuth } from "./auth-context";

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

const emptyVirtualMachineForm: EnvironmentInput = {
  name: "",
  kind: "virtual_machine",
  status: "configured",
  sshHost: "",
  sshPort: 22,
  sshUser: "",
  sshAuthRef: "",
  workdir: "",
  serviceHint: "",
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

function environmentToVMForm(environment: Environment): EnvironmentInput {
  return {
    name: environment.name,
    kind: "virtual_machine",
    status: environment.status || "configured",
    sshHost: environment.virtualMachine?.sshHost || "",
    sshPort: environment.virtualMachine?.sshPort || 22,
    sshUser: environment.virtualMachine?.sshUser || "",
    sshAuthRef: environment.virtualMachine?.sshAuthRef || "",
    workdir: environment.virtualMachine?.workdir || "",
    serviceHint: environment.virtualMachine?.serviceHint || "",
    labels: environment.virtualMachine?.labels || {},
  };
}

export function ClustersPage() {
  const { t } = useMspaceTranslation();
  const queryClient = useQueryClient();
  const auth = useMspaceAuth();
  const { showToast } = useMspaceToast();
  const workspaceId = auth.workspace?.id || "";
  const workspaceReady = auth.status === "signed-in" && Boolean(auth.token && workspaceId);
  const environmentsQueryKey = queryKeys.environments(workspaceId, auth.token);
  const clustersQueryKey = queryKeys.clusters(workspaceId, auth.token);
  const environmentsQuery = useQuery({
    queryKey: environmentsQueryKey,
    queryFn: () => controlPlaneApi.listEnvironments(auth.token, workspaceId),
    enabled: workspaceReady,
  });
  const [settingsCluster, setSettingsCluster] = useState<Cluster | null>(null);
  const [settingsForm, setSettingsForm] = useState<ClusterInput>(emptyClusterForm);
  const [settingsEnvironment, setSettingsEnvironment] = useState<Environment | null>(null);
  const [virtualMachineForm, setVirtualMachineForm] = useState<EnvironmentInput>(emptyVirtualMachineForm);
  const [createVirtualMachineOpen, setCreateVirtualMachineOpen] = useState(false);
  const [defaultImportOpen, setDefaultImportOpen] = useState(false);
  const [defaultDiscoveryRequested, setDefaultDiscoveryRequested] = useState(false);
  const [defaultDiscovery, setDefaultDiscovery] = useState<KubeconfigDiscoveryResult | null>(null);
  const [selectedDefaultPaths, setSelectedDefaultPaths] = useState<string[]>([]);

  const importKubeconfigs = useMutation({
    mutationFn: (paths: string[]) => controlPlaneApi.importKubeconfigFiles(auth.token, workspaceId, paths),
    onSuccess: async (result) => {
      showToast({ tone: "success", description: formatImportResult(result) });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: clustersQueryKey }),
        queryClient.invalidateQueries({ queryKey: environmentsQueryKey }),
      ]);
    },
    onError: (error) => {
      showToast({ tone: "danger", description: error.message });
    },
  });
  const discoverDefaultKubeconfigs = useMutation({
    mutationFn: () => controlPlaneApi.discoverDefaultKubeconfigs(auth.token, workspaceId),
    onSuccess: async (result) => {
      setDefaultDiscovery(result);
      setSelectedDefaultPaths(result.candidates.map((candidate) => candidate.path));
      setDefaultImportOpen(true);
    },
  });
  const updateCluster = useMutation({
    mutationFn: (input: ClusterInput) => {
      if (!settingsCluster) throw new Error(t("clusters.noClusterSelected"));
      return controlPlaneApi.updateCluster(auth.token, workspaceId, settingsCluster.id, input);
    },
    onSuccess: async () => {
      setSettingsCluster(null);
      setSettingsForm(emptyClusterForm);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: clustersQueryKey }),
        queryClient.invalidateQueries({ queryKey: environmentsQueryKey }),
      ]);
    },
  });
  const deleteCluster = useMutation({
    mutationFn: (clusterId: string) => controlPlaneApi.deleteCluster(auth.token, workspaceId, clusterId),
    onSuccess: async () => {
      setSettingsCluster(null);
      setSettingsForm(emptyClusterForm);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: clustersQueryKey }),
        queryClient.invalidateQueries({ queryKey: environmentsQueryKey }),
      ]);
    },
  });
  const createEnvironment = useMutation({
    mutationFn: (input: EnvironmentInput) => controlPlaneApi.createEnvironment(auth.token, workspaceId, input),
    onSuccess: async () => {
      setCreateVirtualMachineOpen(false);
      setVirtualMachineForm(emptyVirtualMachineForm);
      await queryClient.invalidateQueries({ queryKey: environmentsQueryKey });
    },
  });
  const updateEnvironment = useMutation({
    mutationFn: (input: EnvironmentInput) => {
      if (!settingsEnvironment) throw new Error(t("clusters.noEnvironmentSelected"));
      return controlPlaneApi.updateEnvironment(auth.token, workspaceId, settingsEnvironment.id, input);
    },
    onSuccess: async () => {
      setSettingsEnvironment(null);
      setVirtualMachineForm(emptyVirtualMachineForm);
      await queryClient.invalidateQueries({ queryKey: environmentsQueryKey });
    },
  });
  const deleteEnvironment = useMutation({
    mutationFn: (environmentId: string) => controlPlaneApi.deleteEnvironment(auth.token, workspaceId, environmentId),
    onSuccess: async () => {
      setSettingsEnvironment(null);
      setVirtualMachineForm(emptyVirtualMachineForm);
      await queryClient.invalidateQueries({ queryKey: environmentsQueryKey });
    },
  });

  const environments = environmentsQuery.data || [];
  const kubernetesEnvironments = environments.filter((environment) => environment.kind === "kubernetes");
  const clusters = kubernetesEnvironments.map(clusterFromEnvironment).filter(Boolean) as Cluster[];
  const canSave = canSubmitCluster(settingsForm);
  const settingsClusterInUse = Boolean(
    settingsCluster && (settingsCluster.projectCount > 0 || settingsCluster.environmentCount > 0),
  );
  const canSaveVirtualMachine = canSubmitVirtualMachine(virtualMachineForm);
  const settingsEnvironmentInUse = Boolean(
    settingsEnvironment &&
      (settingsEnvironment.issueEnvironmentCount > 0 ||
        settingsEnvironment.testPlanCount > 0 ||
        settingsEnvironment.testRunCount > 0),
  );

  function openSettings(cluster: Cluster) {
    setSettingsCluster(cluster);
    setSettingsForm(clusterToForm(cluster));
    updateCluster.reset();
    deleteCluster.reset();
  }

  function openEnvironmentSettings(environment: Environment) {
    if (environment.kind === "kubernetes") {
      const cluster = clusterFromEnvironment(environment);
      if (cluster) openSettings(cluster);
      return;
    }
    setSettingsEnvironment(environment);
    setVirtualMachineForm(environmentToVMForm(environment));
    updateEnvironment.reset();
    deleteEnvironment.reset();
  }

  function submitSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settingsCluster || !canSave) return;
    updateCluster.mutate(normalizeClusterForm(settingsForm));
  }

  async function importFromPicker() {
    importKubeconfigs.reset();
    if (!workspaceReady) {
      showToast({ tone: "danger", description: t("workspace.signInRequired") });
      return;
    }
    if (!window.mspaceDesktop?.selectKubeconfigFiles) {
      showToast({ tone: "danger", description: t("clusters.pickerDesktopOnly") });
      return;
    }
    const paths = await window.mspaceDesktop.selectKubeconfigFiles();
    if (paths.length === 0) return;
    importKubeconfigs.mutate(paths);
  }

  useEffect(() => {
    if (!workspaceReady) return;
    if (environmentsQuery.isPending || kubernetesEnvironments.length > 0) return;
    if (window.localStorage.getItem(DEFAULT_KUBE_IMPORT_PROMPT_KEY) === "1") return;
    if (defaultDiscoveryRequested || discoverDefaultKubeconfigs.isPending) return;
    setDefaultDiscoveryRequested(true);
    discoverDefaultKubeconfigs.mutate();
  }, [kubernetesEnvironments.length, environmentsQuery.isPending, defaultDiscoveryRequested, discoverDefaultKubeconfigs, workspaceReady]);

  function closeDefaultImportPrompt() {
    window.localStorage.setItem(DEFAULT_KUBE_IMPORT_PROMPT_KEY, "1");
    setDefaultImportOpen(false);
  }

  function openCreateVirtualMachine() {
    setVirtualMachineForm(emptyVirtualMachineForm);
    setCreateVirtualMachineOpen(true);
    createEnvironment.reset();
  }

  return (
    <PageFrame
      title={t("clusters.title")}
      subtitle={t("clusters.subtitle")}
      actions={
        <EnvironmentActionMenu
          workspaceReady={workspaceReady}
          importing={importKubeconfigs.isPending}
          onImportKubeconfig={importFromPicker}
          onAddVirtualMachine={openCreateVirtualMachine}
        />
      }
    >
      {discoverDefaultKubeconfigs.error ? <Notice tone="danger">{discoverDefaultKubeconfigs.error.message}</Notice> : null}

      {environmentsQuery.isPending ? (
        <div className="mt-4 rounded-[10px] bg-[color:var(--surface)] px-4 py-6 text-[13px] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
          {t("clusters.loading")}
        </div>
      ) : environments.length === 0 ? (
        <div className="mt-4">
          <CollectionEmptyState
            icon={Cloud}
            title={t("clusters.emptyTitle")}
            body={t("clusters.emptyBody")}
            action={
              <EnvironmentEmptyActions
                workspaceReady={workspaceReady}
                importing={importKubeconfigs.isPending}
                onImportKubeconfig={importFromPicker}
                onAddVirtualMachine={openCreateVirtualMachine}
              />
            }
          />
        </div>
      ) : (
        <div className="mt-4 rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="grid grid-cols-[minmax(180px,1fr)_minmax(220px,1.4fr)_150px_120px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
            <span>{t("clusters.environment")}</span>
            <span>{t("clusters.access")}</span>
            <span>{t("clusters.usage")}</span>
            <span className="text-right">{t("clusters.actions")}</span>
          </div>
          <div className="divide-y divide-[color:var(--line)]">
            {environments.map((environment) => (
              <EnvironmentRow key={environment.id} environment={environment} onSettings={() => openEnvironmentSettings(environment)} />
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

      {createVirtualMachineOpen ? (
        <VirtualMachineModal
          title={t("clusters.createVirtualMachineTitle")}
          description={t("clusters.virtualMachineDescription")}
          value={virtualMachineForm}
          error={createEnvironment.error}
          isPending={createEnvironment.isPending}
          canSubmit={canSaveVirtualMachine}
          onChange={setVirtualMachineForm}
          onClose={() => setCreateVirtualMachineOpen(false)}
          onSubmit={(event) => {
            event.preventDefault();
            if (!canSaveVirtualMachine) return;
            createEnvironment.mutate(normalizeVirtualMachineForm(virtualMachineForm));
          }}
        />
      ) : null}

      {settingsEnvironment ? (
        <VirtualMachineModal
          title={t("clusters.virtualMachineSettingsTitle")}
          description={t("clusters.virtualMachineDescription")}
          value={virtualMachineForm}
          error={updateEnvironment.error || deleteEnvironment.error}
          isPending={updateEnvironment.isPending}
          canSubmit={canSaveVirtualMachine}
          onChange={setVirtualMachineForm}
          onClose={() => setSettingsEnvironment(null)}
          onSubmit={(event) => {
            event.preventDefault();
            if (!canSaveVirtualMachine) return;
            updateEnvironment.mutate(normalizeVirtualMachineForm(virtualMachineForm));
          }}
          footerStart={
            <Button
              type="button"
              variant="danger"
              disabled={settingsEnvironmentInUse || deleteEnvironment.isPending || updateEnvironment.isPending}
              title={settingsEnvironmentInUse ? t("clusters.inUseDeleteTitle") : undefined}
              onClick={() => deleteEnvironment.mutate(settingsEnvironment.id)}
            >
              <Trash2 data-icon />
              {deleteEnvironment.isPending ? t("clusters.deleting") : t("clusters.deleteEnvironment")}
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
            importKubeconfigs.mutate(selectedDefaultPaths);
          }}
          onSkip={closeDefaultImportPrompt}
        />
      ) : null}
    </PageFrame>
  );
}

function EnvironmentActionMenu(props: {
  workspaceReady: boolean;
  importing: boolean;
  onImportKubeconfig: () => void;
  onAddVirtualMachine: () => void;
}) {
  const { t } = useMspaceTranslation();
  return (
    <div className="inline-flex overflow-hidden rounded-[8px] bg-[color:var(--surface)] shadow-[0_0_0_1px_var(--line)]">
      <button
        type="button"
        className="inline-flex h-8 items-center gap-1.5 px-3 text-[13px] font-medium leading-5 text-[color:var(--text)] transition-[background-color,color,opacity] duration-150 ease-out hover:bg-[color:var(--hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] disabled:pointer-events-none disabled:opacity-50"
        disabled={!props.workspaceReady || props.importing}
        onClick={props.onImportKubeconfig}
      >
        <FileUp data-icon />
        {props.importing ? t("clusters.importing") : t("clusters.importKubeconfig")}
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="grid h-8 w-8 place-items-center border-l border-[color:var(--line)] text-[color:var(--muted)] transition-[background-color,color,opacity] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] disabled:pointer-events-none disabled:opacity-50"
            disabled={!props.workspaceReady}
            aria-label={t("clusters.moreEnvironmentActions")}
          >
            <ChevronDown data-icon />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-64">
          <DropdownMenuLabel>{t("clusters.actionMenuTitle")}</DropdownMenuLabel>
          <DropdownMenuItem disabled={props.importing} onSelect={props.onImportKubeconfig}>
            <FileUp data-icon />
            <span className="grid gap-0.5">
              <span>{props.importing ? t("clusters.importing") : t("clusters.importKubeconfig")}</span>
              <span className="text-[12px] leading-4 text-[color:var(--muted)]">
                {t("clusters.importKubeconfigDescription")}
              </span>
            </span>
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={props.onAddVirtualMachine}>
            <Server data-icon />
            <span className="grid gap-0.5">
              <span>{t("clusters.addVirtualMachine")}</span>
              <span className="text-[12px] leading-4 text-[color:var(--muted)]">
                {t("clusters.addVirtualMachineDescription")}
              </span>
            </span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function EnvironmentEmptyActions(props: {
  workspaceReady: boolean;
  importing: boolean;
  onImportKubeconfig: () => void;
  onAddVirtualMachine: () => void;
}) {
  const { t } = useMspaceTranslation();
  return (
    <div className="grid w-[min(430px,calc(100vw-96px))] grid-cols-1 gap-2 sm:grid-cols-2">
      <button
        type="button"
        className="min-h-[92px] rounded-[9px] bg-[color:var(--paper)] px-3 py-3 text-left shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color,box-shadow,opacity,transform] duration-150 ease-out hover:-translate-y-px hover:bg-[color:var(--hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] disabled:pointer-events-none disabled:opacity-50"
        disabled={!props.workspaceReady || props.importing}
        onClick={props.onImportKubeconfig}
      >
        <span className="flex items-center gap-2 text-[13px] font-semibold leading-5 text-[color:var(--text)]">
          <FileUp data-icon />
          {props.importing ? t("clusters.importing") : t("clusters.emptyKubernetesTitle")}
        </span>
        <span className="mt-1.5 block text-[12px] leading-5 text-[color:var(--muted)] text-pretty">
          {t("clusters.emptyKubernetesBody")}
        </span>
      </button>
      <button
        type="button"
        className="min-h-[92px] rounded-[9px] bg-[color:var(--paper)] px-3 py-3 text-left shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color,box-shadow,opacity,transform] duration-150 ease-out hover:-translate-y-px hover:bg-[color:var(--hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] disabled:pointer-events-none disabled:opacity-50"
        disabled={!props.workspaceReady}
        onClick={props.onAddVirtualMachine}
      >
        <span className="flex items-center gap-2 text-[13px] font-semibold leading-5 text-[color:var(--text)]">
          <Server data-icon />
          {t("clusters.emptyVirtualMachineTitle")}
        </span>
        <span className="mt-1.5 block text-[12px] leading-5 text-[color:var(--muted)] text-pretty">
          {t("clusters.emptyVirtualMachineBody")}
        </span>
      </button>
    </div>
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

function EnvironmentRow(props: { environment: Environment; onSettings: () => void }) {
  const { environment } = props;
  const { t } = useMspaceTranslation();
  const isKubernetes = environment.kind === "kubernetes";
  const exposure = environment.kubernetes?.exposureMode === "ingress" ? "ingress" : "nodeport";
  const accessPrimary = isKubernetes
    ? environment.kubernetes?.kubeconfigPath || t("clusters.notConfigured")
    : `${environment.virtualMachine?.sshUser || "-"}@${environment.virtualMachine?.sshHost || "-"}`;
  const accessSecondary = isKubernetes
    ? environment.kubernetes?.imageRegistryPrefix || t("clusters.notConfigured")
    : environment.virtualMachine?.workdir || environment.virtualMachine?.serviceHint || t("clusters.notConfigured");
  return (
    <article className="grid grid-cols-[minmax(180px,1fr)_minmax(220px,1.4fr)_150px_120px] items-center gap-4 px-4 py-3 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          {isKubernetes ? <Cloud data-icon className="shrink-0 text-[color:var(--muted)]" /> : <Server data-icon className="shrink-0 text-[color:var(--muted)]" />}
          <h3 className="truncate text-[15px] font-semibold leading-6">{environment.name}</h3>
          <StatusBadge value={environment.status || "configured"} />
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          <InlineMeta icon={Clock3}><RelativeTime value={environment.updatedAt} /></InlineMeta>
          <InlineMeta icon={Network}>{isKubernetes ? t("clusters.kindKubernetes") : t("clusters.kindVirtualMachine")}</InlineMeta>
          {isKubernetes ? <InlineMeta icon={Globe2}>{exposure}</InlineMeta> : null}
        </div>
      </div>

      <div className="min-w-0">
        <div className="flex items-center gap-2 text-[13px] font-medium leading-5 text-[color:var(--muted-strong)]">
          <HardDrive data-icon />
          <span className="truncate">{accessPrimary}</span>
        </div>
        <div className="mt-1 flex items-center gap-2 text-[12px] leading-5 text-[color:var(--muted)]">
          <Globe2 data-icon className="shrink-0" />
          <span className="truncate">{accessSecondary}</span>
        </div>
      </div>

      <div className="text-[13px] leading-5 text-[color:var(--muted)]">
        <div>{t("clusters.projects", { count: environment.projectCount })}</div>
        <div>{t("clusters.envs", { count: environment.issueEnvironmentCount + environment.testPlanCount + environment.testRunCount })}</div>
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

function VirtualMachineModal(props: {
  title: string;
  description: string;
  value: EnvironmentInput;
  error?: Error | null;
  isPending: boolean;
  canSubmit: boolean;
  footerStart?: ReactNode;
  onChange: (value: EnvironmentInput) => void;
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
        aria-labelledby="virtual-machine-modal-title"
        className="relative max-h-[calc(100vh-40px)] w-full max-w-[620px] overflow-auto rounded-[12px] bg-[color:var(--paper)] p-5 shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2 id="virtual-machine-modal-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">{props.title}</h2>
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
          <Field label={t("clusters.environmentName")}>
            <Input
              value={props.value.name}
              onChange={(event) => props.onChange({ ...props.value, name: event.target.value })}
              placeholder="ubuntu-deploy-host"
            />
          </Field>
          <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_120px]">
            <Field label={t("clusters.sshHost")}>
              <Input
                value={props.value.sshHost || ""}
                onChange={(event) => props.onChange({ ...props.value, sshHost: event.target.value })}
                placeholder="10.0.0.23"
              />
            </Field>
            <Field label={t("clusters.sshPort")}>
              <Input
                type="number"
                min={1}
                max={65535}
                value={props.value.sshPort || 22}
                onChange={(event) => props.onChange({ ...props.value, sshPort: Number(event.target.value) || 22 })}
              />
            </Field>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <Field label={t("clusters.sshUser")}>
              <Input
                value={props.value.sshUser || ""}
                onChange={(event) => props.onChange({ ...props.value, sshUser: event.target.value })}
                placeholder="root"
              />
            </Field>
            <Field label={t("clusters.sshAuthRef")}>
              <Input
                value={props.value.sshAuthRef || ""}
                onChange={(event) => props.onChange({ ...props.value, sshAuthRef: event.target.value })}
                placeholder={t("clusters.optional")}
              />
            </Field>
          </div>
          <Field label={t("clusters.workdir")}>
            <Input
              value={props.value.workdir || ""}
              onChange={(event) => props.onChange({ ...props.value, workdir: event.target.value })}
              placeholder="/opt/mspace"
            />
          </Field>
          <Field label={t("clusters.serviceHint")}>
            <Input
              value={props.value.serviceHint || ""}
              onChange={(event) => props.onChange({ ...props.value, serviceHint: event.target.value })}
              placeholder={t("clusters.optional")}
            />
          </Field>
          <div className={cn("mt-1 flex flex-wrap gap-2", props.footerStart ? "justify-between" : "justify-end")}>
            {props.footerStart ? <div>{props.footerStart}</div> : null}
            <div className="flex gap-2">
              <Button type="button" variant="secondary" onClick={props.onClose} disabled={props.isPending}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={!props.canSubmit || props.isPending}>
                {props.isPending ? t("clusters.saving") : t("clusters.saveEnvironment")}
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

function normalizeVirtualMachineForm(form: EnvironmentInput): EnvironmentInput {
  const normalized: EnvironmentInput = {
    name: form.name.trim(),
    kind: "virtual_machine",
    status: form.status || "configured",
    sshHost: (form.sshHost || "").trim(),
    sshPort: form.sshPort || 22,
    sshUser: (form.sshUser || "").trim(),
    sshAuthRef: (form.sshAuthRef || "").trim(),
    workdir: (form.workdir || "").trim(),
    serviceHint: (form.serviceHint || "").trim(),
  };
  normalized.virtualMachine = {
    sshHost: normalized.sshHost || "",
    sshPort: normalized.sshPort || 22,
    sshUser: normalized.sshUser || "",
    sshAuthRef: normalized.sshAuthRef || "",
    workdir: normalized.workdir || "",
    serviceHint: normalized.serviceHint || "",
    labels: {},
  };
  return normalized;
}

function canSubmitVirtualMachine(form: EnvironmentInput) {
  const normalized = normalizeVirtualMachineForm(form);
  return Boolean(normalized.name && normalized.sshHost && normalized.sshUser && (normalized.sshPort || 0) > 0);
}

function clusterFromEnvironment(environment: Environment): Cluster | null {
  if (environment.kind !== "kubernetes" || !environment.kubernetes) return null;
  return {
    id: environment.kubernetes.clusterId || environment.id,
    name: environment.name,
    kubeconfigPath: environment.kubernetes.kubeconfigPath,
    kubeContext: environment.kubernetes.kubeContext,
    imageRegistryPrefix: environment.kubernetes.imageRegistryPrefix,
    exposureMode: environment.kubernetes.exposureMode,
    nodeHost: environment.kubernetes.nodeHost,
    previewDomain: environment.kubernetes.previewDomain,
    ingressClass: environment.kubernetes.ingressClass,
    status: environment.status,
    lastCheckedAt: environment.lastCheckedAt,
    projectCount: environment.projectCount,
    environmentCount: environment.issueEnvironmentCount,
    createdAt: environment.createdAt,
    updatedAt: environment.updatedAt,
  };
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
