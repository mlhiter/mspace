import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	Activity,
	ChevronDown,
	ChevronRight,
	CheckCircle2,
	Circle,
	Copy,
	GitCommit,
	GitPullRequest,
	KeyRound,
	ListChecks,
	MailPlus,
	ShieldCheck,
	UsersRound,
	Plus,
	RefreshCw,
	Save,
	ServerCog,
	Settings2,
	SquareTerminal,
	Trash2,
	X,
	type LucideIcon,
} from "lucide-react";
import {
	controlPlaneApi,
	queryKeys,
	type CreateRuntimeRegistrationTokenInput,
	type CreateRuntimeTaskInput,
	type CreateWorkspaceInvitationInput,
	type RuntimeRegistrationToken,
	type RuntimeRegistrationTokenResult,
	type RuntimeTask,
	type RuntimeWorker,
	type WorkspaceInvitation,
	type WorkspaceInvitationResult,
	type WorkspaceMember,
	type UpdateWorkspaceSettingsInput,
} from "@mspace/core";
import {
	Button,
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
	Switch,
	Textarea,
	cn,
} from "@mspace/ui";
import { useMspaceTranslation, t as translate } from "@mspace/i18n";
import { useMspaceAuth } from "./auth-context";
import { RelativeTime } from "./time";

const defaultSettingsForm: UpdateWorkspaceSettingsInput = {
	autoCreateDraftPr: false,
};

const defaultTokenForm: CreateRuntimeRegistrationTokenInput = {
	name: translate("workspaceSettings.modal.workerNamePlaceholder"),
	expiresInHours: 24,
};

const defaultInvitationForm: CreateWorkspaceInvitationInput = {
	email: "",
	role: "member",
	expiresInHours: 168,
};

type RuntimeTaskForm = {
	issueId: string;
	sessionId: string;
	projectId: string;
	kind: string;
	priority: string;
	runtimeMode: "personal" | "team";
	requiredCapabilities: string;
	payload: string;
};

const defaultTaskForm: RuntimeTaskForm = {
	issueId: "",
	sessionId: "",
	projectId: "",
	kind: "protocol_smoke",
	priority: "0",
	runtimeMode: "personal",
	requiredCapabilities: `{"protocolSmoke":true}`,
	payload: `{"source":"workspace-settings"}`,
};

export function WorkspaceSettingsPage() {
	const { t } = useMspaceTranslation();
	const queryClient = useQueryClient();
	const auth = useMspaceAuth();
	const workspaceID = auth.workspace?.id || "";
	const isSignedIn = auth.status === "signed-in" && auth.token !== "";
	const isTeamWorkspace = auth.workspace?.kind === "team";
	const runtimeEnabled = isSignedIn && workspaceID !== "";
	const settingsQueryKey = queryKeys.workspaceSettings(workspaceID, auth.token);
	const defaultRuntimeMode = isTeamWorkspace ? "team" : "personal";
	const runtimeModeLabel = isTeamWorkspace ? t("workspaceSettings.summary.team") : t("workspaceSettings.summary.personal");

	const settingsQuery = useQuery({
		queryKey: settingsQueryKey,
		queryFn: () => controlPlaneApi.getWorkspaceSettings(auth.token, workspaceID),
		enabled: runtimeEnabled,
	});
	const tokensQuery = useQuery({
		queryKey: queryKeys.runtimeRegistrationTokens(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listRuntimeRegistrationTokens(auth.token, workspaceID),
		enabled: runtimeEnabled,
		refetchInterval: runtimeEnabled ? 15_000 : false,
	});
	const membersQuery = useQuery({
		queryKey: queryKeys.workspaceMembers(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listWorkspaceMembers(auth.token, workspaceID),
		enabled: runtimeEnabled && isTeamWorkspace,
		refetchInterval: runtimeEnabled && isTeamWorkspace ? 15_000 : false,
	});
	const invitationsQuery = useQuery({
		queryKey: queryKeys.workspaceInvitations(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listWorkspaceInvitations(auth.token, workspaceID),
		enabled: runtimeEnabled && isTeamWorkspace && (auth.workspace?.role === "owner" || auth.workspace?.role === "admin"),
		refetchInterval: runtimeEnabled && isTeamWorkspace ? 15_000 : false,
	});
	const workersQuery = useQuery({
		queryKey: queryKeys.runtimeWorkers(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listRuntimeWorkers(auth.token, workspaceID),
		enabled: runtimeEnabled,
		refetchInterval: runtimeEnabled ? 5_000 : false,
	});
	const tasksQuery = useQuery({
		queryKey: queryKeys.runtimeTasks(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listRuntimeTasks(auth.token, workspaceID),
		enabled: runtimeEnabled,
		refetchInterval: runtimeEnabled ? 5_000 : false,
	});

	const [form, setForm] = useState<UpdateWorkspaceSettingsInput>(defaultSettingsForm);
	const [invitationModalOpen, setInvitationModalOpen] = useState(false);
	const [invitationForm, setInvitationForm] = useState<CreateWorkspaceInvitationInput>(defaultInvitationForm);
	const [createdInvitation, setCreatedInvitation] = useState<WorkspaceInvitationResult | null>(null);
	const [tokenModalOpen, setTokenModalOpen] = useState(false);
	const [tokenForm, setTokenForm] = useState<CreateRuntimeRegistrationTokenInput>(defaultTokenForm);
	const [createdToken, setCreatedToken] = useState<RuntimeRegistrationTokenResult | null>(null);
	const [copyState, setCopyState] = useState("");
	const [dockerWorkerStatus, setDockerWorkerStatus] = useState("");
	const [dockerWorkerError, setDockerWorkerError] = useState("");
	const [taskModalOpen, setTaskModalOpen] = useState(false);
	const [taskForm, setTaskForm] = useState<RuntimeTaskForm>(defaultTaskForm);
	const [taskFormError, setTaskFormError] = useState("");
	const [selectedTaskID, setSelectedTaskID] = useState("");

	useEffect(() => {
		if (!settingsQuery.data) return;
		setForm({
			autoCreateDraftPr: settingsQuery.data.autoCreateDraftPr,
		});
	}, [settingsQuery.data]);

	useEffect(() => {
		setTaskForm((current) => ({ ...current, runtimeMode: defaultRuntimeMode }));
	}, [defaultRuntimeMode]);

	const saveSettings = useMutation({
		mutationFn: (input: UpdateWorkspaceSettingsInput) =>
			controlPlaneApi.updateWorkspaceSettings(auth.token, workspaceID, input),
		onSuccess: async (settings) => {
			setForm({ autoCreateDraftPr: settings.autoCreateDraftPr });
			await queryClient.invalidateQueries({ queryKey: settingsQueryKey });
		},
	});
	const createInvitation = useMutation({
		mutationFn: (input: CreateWorkspaceInvitationInput) =>
			controlPlaneApi.createWorkspaceInvitation(auth.token, workspaceID, input),
		onSuccess: async (result) => {
			setCreatedInvitation(result);
			setInvitationModalOpen(false);
			setInvitationForm(defaultInvitationForm);
			await Promise.all([
				queryClient.invalidateQueries({ queryKey: queryKeys.workspaceInvitations(workspaceID, auth.token) }),
				queryClient.invalidateQueries({ queryKey: queryKeys.workspaceMembers(workspaceID, auth.token) }),
			]);
		},
	});
	const revokeInvitation = useMutation({
		mutationFn: (invitationID: string) => controlPlaneApi.revokeWorkspaceInvitation(auth.token, workspaceID, invitationID),
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.workspaceInvitations(workspaceID, auth.token) });
		},
	});
	const createToken = useMutation({
		mutationFn: (input: CreateRuntimeRegistrationTokenInput) =>
			controlPlaneApi.createRuntimeRegistrationToken(auth.token, workspaceID, input),
		onSuccess: async (result) => {
			setCreatedToken(result);
			setTokenModalOpen(false);
			setTokenForm(defaultTokenForm);
			await queryClient.invalidateQueries({ queryKey: queryKeys.runtimeRegistrationTokens(workspaceID, auth.token) });
		},
	});
	const startDockerWorker = useMutation({
		mutationFn: async () => {
			if (!window.mspaceDesktop?.startDockerWorker) {
				throw new Error(t("workspaceSettings.dockerWorkerUnavailable"));
			}
			return window.mspaceDesktop.startDockerWorker({
				authToken: auth.token,
				workspaceId: workspaceID,
				mode: defaultRuntimeMode,
				serverUrl: "http://host.docker.internal:8787",
				codex: true,
				workerName: `local-docker-${defaultRuntimeMode}-worker`,
			});
		},
		onMutate: () => {
			setDockerWorkerError("");
			setDockerWorkerStatus(t("workspaceSettings.startingDockerWorker"));
		},
		onSuccess: async (result) => {
			setDockerWorkerStatus(t("workspaceSettings.dockerWorkerStarting", { name: result.containerName }));
			await Promise.all([
				queryClient.invalidateQueries({ queryKey: queryKeys.runtimeRegistrationTokens(workspaceID, auth.token) }),
				queryClient.invalidateQueries({ queryKey: queryKeys.runtimeWorkers(workspaceID, auth.token) }),
			]);
		},
		onError: (error) => {
			setDockerWorkerStatus("");
			setDockerWorkerError(error instanceof Error ? error.message : t("workspaceSettings.dockerWorkerCouldNotStart"));
		},
	});
	const revokeToken = useMutation({
		mutationFn: (tokenID: string) => controlPlaneApi.revokeRuntimeRegistrationToken(auth.token, workspaceID, tokenID),
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.runtimeRegistrationTokens(workspaceID, auth.token) });
		},
	});
	const createTask = useMutation({
		mutationFn: (input: CreateRuntimeTaskInput) => controlPlaneApi.createRuntimeTask(auth.token, workspaceID, input),
		onSuccess: async (task) => {
			setSelectedTaskID(task.id);
			setTaskModalOpen(false);
			setTaskForm({ ...defaultTaskForm, runtimeMode: defaultRuntimeMode });
			setTaskFormError("");
			await queryClient.invalidateQueries({ queryKey: queryKeys.runtimeTasks(workspaceID, auth.token) });
		},
	});
	const cancelTask = useMutation({
		mutationFn: (taskID: string) => controlPlaneApi.cancelRuntimeTask(auth.token, workspaceID, taskID, { reason: t("workspaceSettings.taskCancelReason") }),
		onSuccess: async (_task, taskID) => {
			await Promise.all([
				queryClient.invalidateQueries({ queryKey: queryKeys.runtimeTasks(workspaceID, auth.token) }),
				queryClient.invalidateQueries({ queryKey: queryKeys.runtimeTaskEvents(workspaceID, taskID, auth.token) }),
			]);
		},
	});

	const isDirty = Boolean(settingsQuery.data && form.autoCreateDraftPr !== settingsQuery.data.autoCreateDraftPr);
	const workers = workersQuery.data || [];
	const tokens = tokensQuery.data || [];
	const tasks = tasksQuery.data || [];
	const members = membersQuery.data || [];
	const invitations = invitationsQuery.data || [];
	const canManageWorkspace = auth.workspace?.role === "owner" || auth.workspace?.role === "admin";
	const canStartLocalWorker = runtimeEnabled && canManageWorkspace && Boolean(window.mspaceDesktop?.startDockerWorker);
	const onlineWorkerCount = workers.filter((worker) => worker.status === "online").length;
	const queuedTaskCount = tasks.filter((task) => task.status === "queued").length;
	const runtimeError = isTeamWorkspace
		? membersQuery.error || invitationsQuery.error || tokensQuery.error || workersQuery.error || tasksQuery.error
		: tokensQuery.error || workersQuery.error || tasksQuery.error;

	function refreshRuntime() {
		const refreshes: Array<Promise<unknown>> = [tokensQuery.refetch(), workersQuery.refetch(), tasksQuery.refetch()];
		if (isTeamWorkspace) {
			refreshes.push(membersQuery.refetch(), invitationsQuery.refetch());
		}
		void Promise.all(refreshes);
	}

	function startLocalDockerWorker() {
		if (!canStartLocalWorker || startDockerWorker.isPending) return;
		startDockerWorker.mutate();
	}

	function submitInvitation(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (!runtimeEnabled || !isTeamWorkspace || !canManageWorkspace) return;
		createInvitation.mutate({
			email: invitationForm.email?.trim() || "",
			role: invitationForm.role,
			expiresInHours: invitationForm.expiresInHours,
		});
	}

	function submitToken(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (!runtimeEnabled) return;
		createToken.mutate({
			name: tokenForm.name.trim() || defaultTokenForm.name,
			expiresInHours: tokenForm.expiresInHours,
		});
	}

	function submitTask(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (!runtimeEnabled) return;
		setTaskFormError("");
		try {
			createTask.mutate(normalizeRuntimeTaskForm(taskForm));
		} catch (error) {
			setTaskFormError(error instanceof Error ? error.message : t("workspaceSettings.taskFormInvalid"));
		}
	}

	async function copyCreatedToken() {
		if (!createdToken?.token) return;
		try {
			await navigator.clipboard.writeText(createdToken.token);
			setCopyState(t("workspaceSettings.copied"));
		} catch {
			setCopyState(t("workspaceSettings.copyFailed"));
		}
	}

	async function copyDockerWorkerCommand() {
		if (!createdToken?.token) return;
		try {
			await navigator.clipboard.writeText(buildDockerWorkerCommand(createdToken.token, defaultRuntimeMode));
			setCopyState(t("workspaceSettings.commandCopied"));
		} catch {
			setCopyState(t("workspaceSettings.copyFailed"));
		}
	}

	async function copyCreatedInvitationLink() {
		if (!createdInvitation?.token) return;
		try {
			await navigator.clipboard.writeText(buildInviteLink(createdInvitation.token));
			setCopyState(t("workspaceSettings.copied"));
		} catch {
			setCopyState(t("workspaceSettings.copyFailed"));
		}
	}

	return (
		<PageFrame
			title={t("workspaceSettings.title")}
			subtitle={t("workspaceSettings.subtitle")}
			actions={
				isDirty || saveSettings.isPending ? (
					<Button
						type="button"
						disabled={!isDirty || saveSettings.isPending}
						onClick={() => saveSettings.mutate(form)}
					>
						<Save data-icon />
						{saveSettings.isPending ? t("workspaceSettings.saving") : t("workspaceSettings.save")}
					</Button>
				) : settingsQuery.data ? (
					<span className="inline-flex h-9 items-center gap-1.5 rounded-[7px] px-2.5 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
						<CheckCircle2 data-icon className="size-4 text-[color:var(--success)]" />
						{t("workspaceSettings.saved")}
					</span>
				) : null
			}
		>
			<div className="grid gap-6">
				{settingsQuery.error ? <Notice tone="danger">{settingsQuery.error.message}</Notice> : null}
				{saveSettings.error ? <Notice tone="danger">{saveSettings.error.message}</Notice> : null}
				{runtimeError ? <Notice tone="danger">{runtimeError.message}</Notice> : null}
				{createInvitation.error ? <Notice tone="danger">{createInvitation.error.message}</Notice> : null}
				{revokeInvitation.error ? <Notice tone="danger">{revokeInvitation.error.message}</Notice> : null}
				{dockerWorkerError ? <Notice tone="danger">{dockerWorkerError}</Notice> : null}
				{dockerWorkerStatus ? <Notice>{t("workspaceSettings.workerListUpdateNotice", { status: dockerWorkerStatus })}</Notice> : null}

				<SettingsSection
					title={t("workspaceSettings.section.automation")}
					description={t("workspaceSettings.section.automationDescription")}
					meta={settingsQuery.isFetching ? t("workspaceSettings.section.refreshing") : t("workspaceSettings.section.workspace")}
				>
					<SettingsRow
						icon={GitCommit}
						title={t("workspaceSettings.section.commitCapture")}
						description={t("workspaceSettings.section.commitCaptureDescription")}
						control={<StatusPill>{t("workspaceSettings.section.alwaysOn")}</StatusPill>}
					/>
					<SettingsRow
						icon={GitPullRequest}
						title={t("workspaceSettings.section.draftPullRequests")}
						description={t("workspaceSettings.section.draftPullRequestsDescription")}
						control={
							<Switch
								checked={form.autoCreateDraftPr}
								disabled={!settingsQuery.data || saveSettings.isPending}
								aria-label={t("workspaceSettings.section.autoDraftPr")}
								onCheckedChange={(checked) => setForm({ autoCreateDraftPr: checked })}
							/>
						}
					/>
				</SettingsSection>

				{isTeamWorkspace ? (
					<>
						<SettingsSection
							title={t("workspaceSettings.section.teamAccess")}
							description={t("workspaceSettings.section.teamAccessDescription")}
							meta={runtimeEnabled ? t("workspaceSettings.summary.members") : t("workspaceSettings.section.githubSignInRequired")}
							actions={
								<Button
									type="button"
									variant="secondary"
									size="sm"
									disabled={!runtimeEnabled || !canManageWorkspace}
									onClick={() => setInvitationModalOpen(true)}
								>
									<MailPlus data-icon />
									{t("workspaceSettings.section.inviteMember")}
								</Button>
							}
						>
							{runtimeEnabled ? (
								<div className="grid gap-0">
									<div className="grid gap-3 border-b border-[color:var(--line)] p-4 md:grid-cols-3">
										<RuntimeSummaryCard icon={UsersRound} label={t("workspaceSettings.summary.members")} value={`${members.length}`} meta={canManageWorkspace ? t("workspaceSettings.summary.inviteLinkEnabled") : t("workspaceSettings.summary.inviteLinkAdminOnly")} />
										<RuntimeSummaryCard icon={MailPlus} label={t("workspaceSettings.summary.openInvites")} value={`${activeInvitationCount(invitations)}`} meta={t("workspaceSettings.summary.recentInvitations", { count: invitations.length })} />
										<RuntimeSummaryCard icon={ShieldCheck} label={t("workspaceSettings.summary.yourRole")} value={auth.workspace?.role || "member"} meta={auth.workspace?.name || t("workspaceSettings.summary.selectedWorkspace")} />
									</div>
									<MemberList members={members} loading={membersQuery.isPending && runtimeEnabled} currentUserID={auth.user?.id || ""} />
									<InvitationList
										invitations={invitations}
										loading={invitationsQuery.isPending && runtimeEnabled && canManageWorkspace}
										canManage={canManageWorkspace}
										disabled={revokeInvitation.isPending}
										onRevoke={(invitationID) => revokeInvitation.mutate(invitationID)}
									/>
								</div>
							) : (
								<div className="p-4">
									<Notice>{t("workspaceSettings.notice.signInForTeam")}</Notice>
								</div>
							)}
						</SettingsSection>
					</>
				) : null}
				<SettingsSection
					title={t("workspaceSettings.section.runtime")}
					description={t("workspaceSettings.section.runtimeDescription")}
					meta={runtimeEnabled ? auth.workspace?.name : t("workspaceSettings.section.githubSignInRequired")}
					actions={
						<Button type="button" variant="secondary" size="sm" disabled={!runtimeEnabled} onClick={refreshRuntime}>
							<RefreshCw data-icon />
							{t("workspaceSettings.section.refresh")}
						</Button>
					}
				>
					{runtimeEnabled ? (
						<div className="grid gap-0">
							<div className="grid gap-3 border-b border-[color:var(--line)] p-4 md:grid-cols-3">
								<RuntimeSummaryCard icon={SquareTerminal} label={t("workspaceSettings.summary.runtimeMode")} value={runtimeModeLabel} meta={t("workspaceSettings.summary.serverOwnedQueue")} />
								<RuntimeSummaryCard icon={ServerCog} label={t("workspaceSettings.summary.workers")} value={t("workspaceSettings.summary.online", { count: onlineWorkerCount })} meta={t("workspaceSettings.summary.registered", { count: workers.length })} />
								<RuntimeSummaryCard icon={ListChecks} label={t("workspaceSettings.summary.taskQueue")} value={t("workspaceSettings.summary.queued", { count: queuedTaskCount })} meta={t("workspaceSettings.summary.recentTasks", { count: tasks.length })} />
							</div>
							<SettingsRow
								icon={Settings2}
								title={t("workspaceSettings.section.controlPlane")}
								description={t("workspaceSettings.section.controlPlaneDescription")}
								control={<StatusPill>{auth.workspace?.kind || "workspace"}</StatusPill>}
							/>
							<SettingsRow
								icon={ServerCog}
								title={t("workspaceSettings.section.localDockerWorker")}
								description={t("workspaceSettings.section.localDockerWorkerDescription")}
								control={
									<Button
										type="button"
										variant="secondary"
										size="sm"
										disabled={!canStartLocalWorker || startDockerWorker.isPending}
										onClick={startLocalDockerWorker}
									>
										<SquareTerminal data-icon />
										{startDockerWorker.isPending ? t("workspaceSettings.section.starting") : t("workspaceSettings.section.startWorker")}
									</Button>
								}
							/>
							{!canManageWorkspace ? (
								<div className="border-t border-[color:var(--line)] px-4 py-3">
									<Notice>{t("workspaceSettings.notice.workerPermission")}</Notice>
								</div>
							) : !window.mspaceDesktop?.startDockerWorker ? (
								<div className="border-t border-[color:var(--line)] px-4 py-3">
									<Notice>{t("workspaceSettings.notice.desktopStartup")}</Notice>
								</div>
							) : null}
						</div>
					) : (
						<div className="p-4">
							<Notice>
								{t("workspaceSettings.notice.signInForRuntime")}
							</Notice>
						</div>
					)}
				</SettingsSection>

				<RuntimePanel
					title={t("workspaceSettings.section.advancedCredentials")}
					description={t("workspaceSettings.section.advancedCredentialsDescription")}
					actions={
						<Button type="button" variant="secondary" size="sm" disabled={!runtimeEnabled} onClick={() => setTokenModalOpen(true)}>
							<Plus data-icon />
							{t("workspaceSettings.section.createCredential")}
						</Button>
					}
				>
					<RegistrationTokenList
						tokens={tokens}
						loading={tokensQuery.isPending && runtimeEnabled}
						disabled={!runtimeEnabled || revokeToken.isPending}
						onRevoke={(tokenID) => revokeToken.mutate(tokenID)}
					/>
				</RuntimePanel>

				<RuntimePanel
					title={t("workspaceSettings.section.workers")}
					description={t("workspaceSettings.section.workersDescription")}
				>
					<WorkerList workers={workers} loading={workersQuery.isPending && runtimeEnabled} />
				</RuntimePanel>

				<RuntimePanel
					title={t("workspaceSettings.section.taskQueue")}
					description={t("workspaceSettings.section.taskQueueDescription")}
					actions={
						<Button type="button" variant="secondary" size="sm" disabled={!runtimeEnabled} onClick={() => setTaskModalOpen(true)}>
							<Plus data-icon />
							{t("workspaceSettings.section.queueTask")}
						</Button>
					}
				>
					<TaskList
						tasks={tasks}
						workers={workers}
						loading={tasksQuery.isPending && runtimeEnabled}
						token={auth.token}
						workspaceID={workspaceID}
						selectedTaskID={selectedTaskID}
						onSelectTask={setSelectedTaskID}
						cancellingTaskID={cancelTask.variables || ""}
						onCancelTask={(taskID) => cancelTask.mutate(taskID)}
					/>
				</RuntimePanel>
				{cancelTask.error ? <Notice tone="danger">{cancelTask.error.message}</Notice> : null}

				<SettingsSection
					title={t("workspaceSettings.section.githubIdentity")}
					description={t("workspaceSettings.section.githubIdentityDescription")}
				>
					<SettingsRow
						icon={Settings2}
						title={t("workspaceSettings.section.localGithubCli")}
						description={t("workspaceSettings.section.localGithubCliDescription")}
						control={<StatusPill>{t("workspaceSettings.section.local")}</StatusPill>}
					/>
				</SettingsSection>
			</div>

			{invitationModalOpen ? (
				<Modal title={t("workspaceSettings.modal.inviteTitle")} description={t("workspaceSettings.modal.inviteDescription")} onClose={() => setInvitationModalOpen(false)}>
					<form className="grid gap-4" onSubmit={submitInvitation}>
						{createInvitation.error ? <Notice tone="danger">{createInvitation.error.message}</Notice> : null}
						<Field label={t("workspaceSettings.modal.email")} hint={t("workspaceSettings.modal.emailHint")}>
							<Input
								type="email"
								value={invitationForm.email || ""}
								onChange={(event) => setInvitationForm({ ...invitationForm, email: event.target.value })}
								placeholder="teammate@example.com"
							/>
						</Field>
						<div className="grid gap-3 md:grid-cols-2">
							<Field label={t("workspaceSettings.modal.role")}>
								<Select
									value={invitationForm.role}
									onValueChange={(value) => setInvitationForm({ ...invitationForm, role: value === "admin" ? "admin" : "member" })}
								>
									<SelectTrigger>
										<SelectValue placeholder={t("workspaceSettings.modal.workspaceRole")} />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="member">{t("workspaceSettings.modal.member")}</SelectItem>
										<SelectItem value="admin">{t("workspaceSettings.modal.admin")}</SelectItem>
									</SelectContent>
								</Select>
							</Field>
							<Field label={t("workspaceSettings.modal.expires")}>
								<Select
									value={String(invitationForm.expiresInHours)}
									onValueChange={(value) => setInvitationForm({ ...invitationForm, expiresInHours: Number(value) })}
								>
									<SelectTrigger>
										<SelectValue placeholder={t("workspaceSettings.modal.inviteExpiry")} />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="12">{t("workspaceSettings.modal.hours", { count: 12, suffix: "s" })}</SelectItem>
										<SelectItem value="24">{t("workspaceSettings.modal.hours", { count: 24, suffix: "s" })}</SelectItem>
										<SelectItem value="168">{t("workspaceSettings.modal.days", { count: 7 })}</SelectItem>
										<SelectItem value="720">{t("workspaceSettings.modal.days", { count: 30 })}</SelectItem>
									</SelectContent>
								</Select>
							</Field>
						</div>
						<div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
							<Button type="button" variant="secondary" onClick={() => setInvitationModalOpen(false)}>
								{t("common.cancel")}
							</Button>
							<Button type="submit" disabled={!runtimeEnabled || !isTeamWorkspace || !canManageWorkspace || createInvitation.isPending}>
								<MailPlus data-icon />
								{createInvitation.isPending ? t("common.creating") : t("workspaceSettings.modal.createInvite")}
							</Button>
						</div>
					</form>
				</Modal>
			) : null}

			{createdInvitation ? (
				<Modal title={t("workspaceSettings.modal.inviteLinkCreatedTitle")} description={t("workspaceSettings.modal.inviteLinkCreatedDescription")} onClose={() => {
					setCreatedInvitation(null);
					setCopyState("");
				}}>
					<div className="grid gap-4">
						<div className="rounded-[9px] bg-[color:var(--code-bg)] px-3 py-3 font-mono text-[12px] leading-6 text-[color:var(--code-text)]">
							{buildInviteLink(createdInvitation.token)}
						</div>
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">
							{t("workspaceSettings.modal.inviteSummary", {
								role: createdInvitation.invitation.role,
								email: createdInvitation.invitation.email || t("workspaceSettings.modal.anySignedInTeammate"),
							})}{" "}
							<RelativeTime value={createdInvitation.invitation.expiresAt} />.
						</div>
						<div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
							<Button type="button" variant="secondary" onClick={copyCreatedInvitationLink}>
								<Copy data-icon />
								{copyState || t("workspaceSettings.modal.copyInviteLink")}
							</Button>
							<Button type="button" onClick={() => {
								setCreatedInvitation(null);
								setCopyState("");
							}}>
								{t("workspaceSettings.modal.done")}
							</Button>
						</div>
					</div>
				</Modal>
			) : null}

			{tokenModalOpen ? (
				<Modal title={t("workspaceSettings.modal.tokenTitle")} description={t("workspaceSettings.modal.tokenDescription")} onClose={() => setTokenModalOpen(false)}>
					<form className="grid gap-4" onSubmit={submitToken}>
						{createToken.error ? <Notice tone="danger">{createToken.error.message}</Notice> : null}
						<Field label={t("workspaceSettings.modal.name")} hint={t("workspaceSettings.modal.tokenNameHint")}>
							<Input
								value={tokenForm.name}
								onChange={(event) => setTokenForm({ ...tokenForm, name: event.target.value })}
								placeholder={t("workspaceSettings.modal.workerNamePlaceholder")}
							/>
						</Field>
						<Field label={t("workspaceSettings.modal.expires")}>
							<Select
								value={String(tokenForm.expiresInHours)}
								onValueChange={(value) => setTokenForm({ ...tokenForm, expiresInHours: Number(value) })}
							>
								<SelectTrigger>
									<SelectValue placeholder={t("workspaceSettings.modal.tokenExpiry")} />
								</SelectTrigger>
								<SelectContent>
									<SelectItem value="1">{t("workspaceSettings.modal.hours", { count: 1, suffix: "" })}</SelectItem>
									<SelectItem value="12">{t("workspaceSettings.modal.hours", { count: 12, suffix: "s" })}</SelectItem>
									<SelectItem value="24">{t("workspaceSettings.modal.hours", { count: 24, suffix: "s" })}</SelectItem>
									<SelectItem value="168">{t("workspaceSettings.modal.days", { count: 7 })}</SelectItem>
									<SelectItem value="720">{t("workspaceSettings.modal.days", { count: 30 })}</SelectItem>
								</SelectContent>
							</Select>
						</Field>
						<div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
							<Button type="button" variant="secondary" onClick={() => setTokenModalOpen(false)}>
								{t("common.cancel")}
							</Button>
							<Button type="submit" disabled={!runtimeEnabled || createToken.isPending}>
								<KeyRound data-icon />
								{createToken.isPending ? t("common.creating") : t("workspaceSettings.section.createCredential")}
							</Button>
						</div>
					</form>
				</Modal>
			) : null}

			{createdToken ? (
				<Modal title={t("workspaceSettings.modal.credentialCreatedTitle")} description={t("workspaceSettings.modal.credentialCreatedDescription")} onClose={() => {
					setCreatedToken(null);
					setCopyState("");
				}}>
					<div className="grid gap-4">
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">
							{t("workspaceSettings.modal.prefixExpires", { prefix: createdToken.registrationToken.tokenPrefix })}{" "}
							<RelativeTime value={createdToken.registrationToken.expiresAt} />.
						</div>
						<div className="grid gap-2">
							<div className="text-[12px] font-medium leading-5 text-[color:var(--muted)]">{t("workspaceSettings.modal.setupCommand")}</div>
							<div className="overflow-auto rounded-[9px] bg-[color:var(--code-bg)] px-3 py-3 font-mono text-[12px] leading-6 text-[color:var(--code-text)]">
								{buildDockerWorkerCommand(createdToken.token, defaultRuntimeMode)}
							</div>
							<div className="text-[12px] leading-5 text-[color:var(--muted)]">
								{t("workspaceSettings.modal.dryRunWorkerDescription")}
							</div>
						</div>
						<details className="rounded-[8px] bg-[color:var(--block)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
							<summary className="cursor-pointer text-[12px] font-medium leading-5 text-[color:var(--muted)]">{t("workspaceSettings.modal.showRawCredential")}</summary>
							<div className="mt-2 rounded-[7px] bg-[color:var(--code-bg)] px-3 py-3 font-mono text-[12px] leading-6 text-[color:var(--code-text)]">
								{createdToken.token}
							</div>
						</details>
						<div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
							<Button type="button" variant="secondary" onClick={copyCreatedToken}>
								<Copy data-icon />
								{copyState || t("workspaceSettings.modal.copyRawCredential")}
							</Button>
							<Button type="button" onClick={copyDockerWorkerCommand}>
								<SquareTerminal data-icon />
								{t("workspaceSettings.modal.copyCommand")}
							</Button>
							<Button type="button" variant="secondary" onClick={() => {
								setCreatedToken(null);
								setCopyState("");
							}}>
								{t("workspaceSettings.modal.done")}
							</Button>
						</div>
					</div>
				</Modal>
			) : null}

			{taskModalOpen ? (
				<Modal title={t("workspaceSettings.modal.taskTitle")} description={t("workspaceSettings.modal.taskDescription")} onClose={() => setTaskModalOpen(false)}>
					<form className="grid gap-4" onSubmit={submitTask}>
						{taskFormError ? <Notice tone="danger">{taskFormError}</Notice> : null}
						{createTask.error ? <Notice tone="danger">{createTask.error.message}</Notice> : null}
						<div className="grid gap-3 md:grid-cols-2">
							<Field label={t("workspaceSettings.modal.kind")}>
								<Input value={taskForm.kind} onChange={(event) => setTaskForm({ ...taskForm, kind: event.target.value })} />
							</Field>
							<Field label={t("workspaceSettings.modal.runtimeMode")}>
								<Select
									value={taskForm.runtimeMode}
									onValueChange={(value) => setTaskForm({ ...taskForm, runtimeMode: value === "personal" ? "personal" : "team" })}
								>
									<SelectTrigger>
										<SelectValue />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="team">{t("workspaceSettings.summary.team")}</SelectItem>
										<SelectItem value="personal">{t("workspaceSettings.summary.personal")}</SelectItem>
									</SelectContent>
								</Select>
							</Field>
							<Field label={t("workspaceSettings.modal.priority")}>
								<Input type="number" min={0} value={taskForm.priority} onChange={(event) => setTaskForm({ ...taskForm, priority: event.target.value })} />
							</Field>
							<Field label={t("workspaceSettings.modal.issueId")}>
								<Input value={taskForm.issueId} onChange={(event) => setTaskForm({ ...taskForm, issueId: event.target.value })} placeholder={t("workspaceSettings.modal.optional")} />
							</Field>
							<Field label={t("workspaceSettings.modal.projectId")}>
								<Input value={taskForm.projectId} onChange={(event) => setTaskForm({ ...taskForm, projectId: event.target.value })} placeholder={t("workspaceSettings.modal.optional")} />
							</Field>
							<Field label={t("workspaceSettings.modal.sessionId")}>
								<Input value={taskForm.sessionId} onChange={(event) => setTaskForm({ ...taskForm, sessionId: event.target.value })} placeholder={t("workspaceSettings.modal.optional")} />
							</Field>
						</div>
						<Field label={t("workspaceSettings.modal.requiredCapabilities")} hint={t("workspaceSettings.modal.requiredCapabilitiesHint", { example: '{"protocolSmoke":true}' })}>
							<Textarea value={taskForm.requiredCapabilities} onChange={(event) => setTaskForm({ ...taskForm, requiredCapabilities: event.target.value })} />
						</Field>
						<Field label={t("workspaceSettings.modal.payload")} hint={t("workspaceSettings.modal.payloadHint")}>
							<Textarea value={taskForm.payload} onChange={(event) => setTaskForm({ ...taskForm, payload: event.target.value })} />
						</Field>
						<div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
							<Button type="button" variant="secondary" onClick={() => setTaskModalOpen(false)}>
								{t("common.cancel")}
							</Button>
							<Button type="submit" disabled={!runtimeEnabled || createTask.isPending}>
								<ListChecks data-icon />
								{createTask.isPending ? t("workspaceSettings.modal.queueing") : t("workspaceSettings.section.queueTask")}
							</Button>
						</div>
					</form>
				</Modal>
			) : null}
		</PageFrame>
	);
}

function SettingsSection(props: {
	title: string;
	description: string;
	meta?: string;
	actions?: ReactNode;
	children: ReactNode;
}) {
	return (
		<section className="grid gap-2">
			<div className="flex min-w-0 items-end justify-between gap-4 px-0.5">
				<div className="min-w-0">
					<h2 className="text-[14px] font-semibold leading-5 text-[color:var(--text)]">{props.title}</h2>
					<p className="mt-1 max-w-[72ch] text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</p>
				</div>
				<div className="flex shrink-0 items-center gap-2">
					{props.meta ? <span className="text-[12px] leading-5 text-[color:var(--faint)]">{props.meta}</span> : null}
					{props.actions}
				</div>
			</div>
			<div className="overflow-hidden rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
				{props.children}
			</div>
		</section>
	);
}

function SettingsRow(props: {
	icon: LucideIcon;
	title: string;
	description: string;
	control: ReactNode;
}) {
	const Icon = props.icon;
	return (
		<div className="grid min-w-0 gap-3 border-b border-[color:var(--line)] px-4 py-3.5 last:border-b-0 md:grid-cols-[32px_minmax(0,1fr)_auto] md:items-center">
			<span className="grid size-8 place-items-center rounded-[8px] bg-[color:var(--block)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
				<Icon data-icon />
			</span>
			<span className="min-w-0">
				<span className="block text-[13px] font-medium leading-5 text-[color:var(--text)]">{props.title}</span>
				<span className="mt-1 block max-w-[76ch] text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</span>
			</span>
			<div className="justify-self-start md:justify-self-end">{props.control}</div>
		</div>
	);
}

function RuntimePanel(props: {
	title: string;
	description: string;
	actions?: ReactNode;
	children: ReactNode;
}) {
	return (
		<section className="grid gap-2">
			<div className="flex min-w-0 items-end justify-between gap-4 px-0.5">
				<div className="min-w-0">
					<h2 className="text-[14px] font-semibold leading-5 text-[color:var(--text)]">{props.title}</h2>
					<p className="mt-1 max-w-[72ch] text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</p>
				</div>
				{props.actions ? <div className="shrink-0">{props.actions}</div> : null}
			</div>
			<div className="overflow-hidden rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
				{props.children}
			</div>
		</section>
	);
}

function RuntimeSummaryCard(props: { icon: LucideIcon; label: string; value: string; meta: string }) {
	const Icon = props.icon;
	return (
		<div className="min-w-0 rounded-[9px] bg-[color:var(--block)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
			<div className="flex items-center gap-2 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
				<Icon data-icon />
				{props.label}
			</div>
			<div className="mt-2 truncate text-[18px] font-semibold leading-6 text-[color:var(--text)]">{props.value}</div>
			<div className="mt-1 truncate text-[12px] leading-5 text-[color:var(--muted)]">{props.meta}</div>
		</div>
	);
}

function MemberList(props: { members: WorkspaceMember[]; loading: boolean; currentUserID: string }) {
	const { t } = useMspaceTranslation();

	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingMembers")}</LoadingBlock>;
	if (props.members.length === 0) {
		return <EmptyRuntimeBlock icon={UsersRound} title={t("workspaceSettings.list.noMembersTitle")} body={t("workspaceSettings.list.noMembersBody")} />;
	}
	return (
		<div>
			<TableHeader columns="grid-cols-[minmax(190px,1.1fr)_150px_minmax(190px,1fr)_150px]">
				<span>{t("workspaceSettings.list.member")}</span>
				<span>{t("workspaceSettings.list.role")}</span>
				<span>{t("workspaceSettings.list.github")}</span>
				<span>{t("workspaceSettings.list.joined")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.members.map((member) => (
					<div key={member.id} className="grid grid-cols-[minmax(190px,1.1fr)_150px_minmax(190px,1fr)_150px] items-center gap-4 px-4 py-3">
						<div className="flex min-w-0 items-center gap-2.5">
							<MemberAvatar member={member} />
							<div className="min-w-0">
								<div className="truncate text-[13px] font-medium text-[color:var(--text)]">
									{member.name || member.email || t("workspaceSettings.list.mspaceUser")}
									{member.userId === props.currentUserID ? <span className="ml-1 text-[12px] font-normal text-[color:var(--muted)]">({t("workspaceSettings.list.you")})</span> : null}
								</div>
								<div className="truncate text-[12px] leading-5 text-[color:var(--muted)]">{member.email || t("workspaceSettings.list.noPublicEmail")}</div>
							</div>
						</div>
						<RolePill role={member.role} />
						<span className="truncate font-mono text-[12px] text-[color:var(--muted)]">{member.identityLogin || t("workspaceSettings.list.notLinked")}</span>
						<span className="text-[12px] leading-5 text-[color:var(--muted)]"><RelativeTime value={member.createdAt} /></span>
					</div>
				))}
			</div>
		</div>
	);
}

function InvitationList(props: {
	invitations: WorkspaceInvitation[];
	loading: boolean;
	canManage: boolean;
	disabled: boolean;
	onRevoke: (invitationID: string) => void;
}) {
	const { t } = useMspaceTranslation();

	if (!props.canManage) {
		return (
			<div className="border-t border-[color:var(--line)] p-4">
				<Notice>{t("workspaceSettings.notice.invitePermission")}</Notice>
			</div>
		);
	}
	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingInvitations")}</LoadingBlock>;
	if (props.invitations.length === 0) {
		return <EmptyRuntimeBlock icon={MailPlus} title={t("workspaceSettings.list.noInvitationsTitle")} body={t("workspaceSettings.list.noInvitationsBody")} />;
	}
	return (
		<div className="border-t border-[color:var(--line)]">
			<TableHeader columns="grid-cols-[minmax(190px,1fr)_120px_150px_130px_96px]">
				<span>{t("workspaceSettings.list.invitation")}</span>
				<span>{t("workspaceSettings.list.role")}</span>
				<span>{t("workspaceSettings.list.expires")}</span>
				<span>{t("workspaceSettings.list.status")}</span>
				<span className="text-right">{t("workspaceSettings.list.actions")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.invitations.map((invitation) => {
					const status = invitationStatus(invitation);
					return (
						<div key={invitation.id} className="grid grid-cols-[minmax(190px,1fr)_120px_150px_130px_96px] items-center gap-4 px-4 py-3 text-[13px]">
							<div className="min-w-0">
								<div className="truncate font-medium text-[color:var(--text)]">{invitation.email || t("workspaceSettings.list.openInviteLink")}</div>
								<InlineMeta icon={MailPlus}>{t("workspaceSettings.list.prefix")} {invitation.tokenPrefix}</InlineMeta>
							</div>
							<RolePill role={invitation.role} />
							<span className="text-[12px] text-[color:var(--muted)]"><RelativeTime value={invitation.expiresAt} /></span>
							<StatusPill>{status}</StatusPill>
							<div className="flex justify-end">
								<Button
									type="button"
									variant="ghost"
									size="icon"
									aria-label={t("workspaceSettings.list.revokeInvite", { name: invitation.email || invitation.tokenPrefix })}
									disabled={props.disabled || status !== "pending"}
									onClick={() => props.onRevoke(invitation.id)}
								>
									<Trash2 data-icon />
								</Button>
							</div>
						</div>
					);
				})}
			</div>
		</div>
	);
}

function RegistrationTokenList(props: {
	tokens: RuntimeRegistrationToken[];
	loading: boolean;
	disabled: boolean;
	onRevoke: (tokenID: string) => void;
}) {
	const { t } = useMspaceTranslation();

	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingCredentials")}</LoadingBlock>;
	if (props.tokens.length === 0) {
		return <EmptyRuntimeBlock icon={KeyRound} title={t("workspaceSettings.list.noCredentialsTitle")} body={t("workspaceSettings.list.noCredentialsBody")} />;
	}
	return (
		<div>
			<TableHeader columns="grid-cols-[minmax(160px,1.2fr)_120px_150px_150px_110px_96px]">
				<span>{t("workspaceSettings.list.name")}</span>
				<span>{t("workspaceSettings.list.prefix")}</span>
				<span>{t("workspaceSettings.list.expires")}</span>
				<span>{t("workspaceSettings.list.lastUsed")}</span>
				<span>{t("workspaceSettings.list.status")}</span>
				<span className="text-right">{t("workspaceSettings.list.actions")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.tokens.map((token) => (
					<div key={token.id} className="grid grid-cols-[minmax(160px,1.2fr)_120px_150px_150px_110px_96px] items-center gap-4 px-4 py-3 text-[13px]">
						<div className="min-w-0">
							<div className="truncate font-medium text-[color:var(--text)]">{token.name}</div>
							<InlineMeta icon={KeyRound}>{t("workspaceSettings.list.created")} <RelativeTime value={token.createdAt} /></InlineMeta>
						</div>
						<span className="truncate font-mono text-[12px] text-[color:var(--muted)]">{token.tokenPrefix}</span>
						<span className="text-[12px] text-[color:var(--muted)]"><RelativeTime value={token.expiresAt} /></span>
						<span className="text-[12px] text-[color:var(--muted)]">{token.lastUsedAt ? <RelativeTime value={token.lastUsedAt} /> : t("workspaceSettings.list.never")}</span>
						<TokenStatus token={token} />
						<div className="flex justify-end">
							<Button
								type="button"
								variant="ghost"
								size="icon"
								aria-label={t("workspaceSettings.list.revokeCredential", { name: token.name })}
								disabled={props.disabled || token.revoked}
								onClick={() => props.onRevoke(token.id)}
							>
								<Trash2 data-icon />
							</Button>
						</div>
					</div>
				))}
			</div>
		</div>
	);
}

function WorkerList(props: { workers: RuntimeWorker[]; loading: boolean }) {
	const { t } = useMspaceTranslation();

	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingWorkers")}</LoadingBlock>;
	if (props.workers.length === 0) {
		return <EmptyRuntimeBlock icon={ServerCog} title={t("workspaceSettings.list.noWorkersTitle")} body={t("workspaceSettings.list.noWorkersBody")} />;
	}
	return (
		<div>
			<TableHeader columns="grid-cols-[minmax(170px,1.1fr)_150px_120px_minmax(220px,1.2fr)_150px]">
				<span>{t("workspaceSettings.list.worker")}</span>
				<span>{t("workspaceSettings.list.mode")}</span>
				<span>{t("workspaceSettings.list.load")}</span>
				<span>{t("workspaceSettings.list.capabilities")}</span>
				<span>{t("workspaceSettings.list.lastSeen")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.workers.map((worker) => (
					<div key={worker.id} className="grid grid-cols-[minmax(170px,1.1fr)_150px_120px_minmax(220px,1.2fr)_150px] items-center gap-4 px-4 py-3">
						<div className="min-w-0">
							<div className="truncate text-[13px] font-medium text-[color:var(--text)]">{worker.name}</div>
							<div className="mt-1 truncate text-[12px] leading-5 text-[color:var(--muted)]">{worker.version || t("workspaceSettings.list.versionNotReported")}</div>
						</div>
						<div className="flex min-w-0 flex-wrap items-center gap-1.5">
							<RuntimeStatusPill status={worker.status} />
							<StatusPill>{worker.mode}</StatusPill>
						</div>
						<span className="font-mono text-[12px] tabular-nums text-[color:var(--muted)]">{worker.currentLoad}</span>
						<span className="truncate font-mono text-[12px] leading-5 text-[color:var(--muted)]">{jsonSummary(worker.capabilities)}</span>
						<span className="text-[12px] leading-5 text-[color:var(--muted)]"><RelativeTime value={worker.lastSeenAt} /></span>
					</div>
				))}
			</div>
		</div>
	);
}

function TaskList(props: {
	tasks: RuntimeTask[];
	workers: RuntimeWorker[];
	loading: boolean;
	token: string;
	workspaceID: string;
	selectedTaskID: string;
	onSelectTask: (taskID: string) => void;
	cancellingTaskID?: string;
	onCancelTask: (taskID: string) => void;
}) {
	const { t } = useMspaceTranslation();
	const workerByID = useMemo(() => new Map(props.workers.map((worker) => [worker.id, worker])), [props.workers]);
	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingTasks")}</LoadingBlock>;
	if (props.tasks.length === 0) {
		return <EmptyRuntimeBlock icon={ListChecks} title={t("workspaceSettings.list.noTasksTitle")} body={t("workspaceSettings.list.noTasksBody")} />;
	}
	return (
		<div>
			<TableHeader columns="grid-cols-[minmax(180px,1fr)_130px_110px_minmax(180px,1fr)_150px_92px]">
				<span>{t("workspaceSettings.list.task")}</span>
				<span>{t("workspaceSettings.list.status")}</span>
				<span>{t("workspaceSettings.list.priority")}</span>
				<span>{t("workspaceSettings.list.worker")}</span>
				<span>{t("workspaceSettings.list.updated")}</span>
				<span>{t("workspaceSettings.list.action")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.tasks.map((task) => {
					const worker = task.claimedByWorkerId ? workerByID.get(task.claimedByWorkerId) : undefined;
					const selected = props.selectedTaskID === task.id;
					const cancellable = ["queued", "claimed", "running"].includes(task.status);
					return (
						<div key={task.id}>
							<div className="grid w-full grid-cols-[minmax(180px,1fr)_130px_110px_minmax(180px,1fr)_150px_92px] items-center gap-4 px-4 py-3 transition-colors hover:bg-[color:var(--hover)]">
								<button
									type="button"
									className="min-w-0 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)]"
									aria-expanded={selected}
									onClick={() => props.onSelectTask(selected ? "" : task.id)}
								>
									<div className="min-w-0">
										<div className="flex min-w-0 items-center gap-2">
											{selected ? <ChevronDown data-icon className="shrink-0 text-[color:var(--faint)]" /> : <ChevronRight data-icon className="shrink-0 text-[color:var(--faint)]" />}
											<span className="truncate text-[13px] font-medium text-[color:var(--text)]">{task.kind}</span>
											<StatusPill>{task.runtimeMode}</StatusPill>
										</div>
										<div className="mt-1 truncate pl-6 text-[12px] leading-5 text-[color:var(--muted)]">
											{task.issueId || task.projectId || task.sessionId || task.id}
										</div>
									</div>
								</button>
								<RuntimeStatusPill status={task.status} />
								<span className="font-mono text-[12px] tabular-nums text-[color:var(--muted)]">{task.priority}</span>
								<span className="truncate text-[12px] leading-5 text-[color:var(--muted)]">{worker?.name || task.claimedByWorkerId || t("workspaceSettings.list.unclaimed")}</span>
								<span className="text-[12px] leading-5 text-[color:var(--muted)]"><RelativeTime value={task.updatedAt} /></span>
								<Button type="button" variant="ghost" size="sm" disabled={!cancellable || props.cancellingTaskID === task.id} onClick={() => props.onCancelTask(task.id)}>
									<X data-icon />
									{props.cancellingTaskID === task.id ? t("workspaceSettings.list.cancelling") : t("workspaceSettings.list.cancel")}
								</Button>
							</div>
							{selected ? <RuntimeTaskEvidence task={task} token={props.token} workspaceID={props.workspaceID} /> : null}
						</div>
					);
				})}
			</div>
		</div>
	);
}

function RuntimeTaskEvidence(props: { task: RuntimeTask; token: string; workspaceID: string }) {
	const { t } = useMspaceTranslation();
	const enabled = Boolean(props.token && props.workspaceID && props.task.id);
	const eventsQuery = useQuery({
		queryKey: queryKeys.runtimeTaskEvents(props.workspaceID, props.task.id, props.token),
		queryFn: () => controlPlaneApi.listRuntimeTaskEvents(props.token, props.workspaceID, props.task.id),
		enabled,
		refetchInterval: enabled && ["queued", "claimed", "running"].includes(props.task.status) ? 2_500 : false,
	});
	const logsQuery = useQuery({
		queryKey: queryKeys.runtimeTaskLogs(props.workspaceID, props.task.id, props.token),
		queryFn: () => controlPlaneApi.listRuntimeTaskLogs(props.token, props.workspaceID, props.task.id),
		enabled,
		refetchInterval: enabled && ["claimed", "running"].includes(props.task.status) ? 2_500 : false,
	});
	const events = eventsQuery.data || [];
	const logs = logsQuery.data || [];
	const failed = eventsQuery.error || logsQuery.error;

	return (
		<div className="border-t border-[color:var(--line)] bg-[color:var(--block)] px-4 py-4">
			{failed ? <Notice tone="danger">{failed.message}</Notice> : null}
			<div className="grid gap-4 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.4fr)]">
				<div className="min-w-0">
					<div className="mb-2 flex items-center gap-2 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
						<Activity data-icon />
						{t("workspaceSettings.list.events")}
					</div>
					{eventsQuery.isPending ? (
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("workspaceSettings.list.loadingEvents")}</div>
					) : events.length === 0 ? (
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("workspaceSettings.list.noTaskEvents")}</div>
					) : (
						<div className="grid gap-2">
							{events.map((event) => (
								<div key={event.id} className="rounded-[7px] bg-[color:var(--surface)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
									<div className="flex min-w-0 items-center justify-between gap-2">
										<span className="truncate text-[12px] font-medium leading-5 text-[color:var(--text)]">{runtimeStatusLabel(event.kind)}</span>
										<span className="shrink-0 text-[11px] leading-5 text-[color:var(--faint)]"><RelativeTime value={event.createdAt} /></span>
									</div>
									<div className="mt-1 truncate font-mono text-[11px] leading-5 text-[color:var(--muted)]">{jsonSummary(event.payload)}</div>
								</div>
							))}
						</div>
					)}
				</div>
				<div className="min-w-0">
					<div className="mb-2 flex items-center gap-2 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
						<SquareTerminal data-icon />
						{t("workspaceSettings.list.logs")}
					</div>
					{logsQuery.isPending ? (
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("workspaceSettings.list.loadingLogs")}</div>
					) : logs.length === 0 ? (
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("workspaceSettings.list.noWorkerLogs")}</div>
					) : (
						<div className="max-h-[360px] overflow-auto rounded-[8px] bg-[color:var(--code-bg)] px-3 py-2 font-mono text-[12px] leading-5 text-[color:var(--code-text)]">
							{logs.map((log) => (
								<div key={log.id} className="grid grid-cols-[92px_92px_minmax(0,1fr)] gap-2 border-b border-white/10 py-1 last:border-b-0">
									<span className="truncate text-white/45">{timeLabel(log.createdAt)}</span>
									<span className="truncate text-white/65">{log.stream}</span>
									<span className="min-w-0 whitespace-pre-wrap break-words">{log.message}</span>
								</div>
							))}
						</div>
					)}
				</div>
			</div>
			<div className="mt-4 grid gap-2 rounded-[8px] bg-[color:var(--surface)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
				<div className="grid gap-2 md:grid-cols-2">
					<RuntimeJSONBlock label={t("workspaceSettings.modal.requiredCapabilities")} value={props.task.requiredCapabilities} />
					<RuntimeJSONBlock label={t("workspaceSettings.modal.payload")} value={props.task.payload} />
				</div>
				{props.task.error ? <RuntimeJSONBlock label={t("workspaceSettings.list.error")} value={{ message: props.task.error }} /> : null}
				{Object.keys(props.task.result || {}).length > 0 ? <RuntimeJSONBlock label={t("workspaceSettings.list.result")} value={props.task.result} /> : null}
			</div>
		</div>
	);
}

function RuntimeJSONBlock(props: { label: string; value: Record<string, unknown> }) {
	return (
		<div className="min-w-0">
			<div className="mb-1 text-[11px] font-medium leading-5 text-[color:var(--faint)]">{props.label}</div>
			<pre className="max-h-[160px] overflow-auto rounded-[6px] bg-[color:var(--block)] px-2.5 py-2 text-[11px] leading-5 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
				{JSON.stringify(props.value || {}, null, 2)}
			</pre>
		</div>
	);
}

function TableHeader(props: { columns: string; children: ReactNode }) {
	return (
		<div className={cn("grid gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]", props.columns)}>
			{props.children}
		</div>
	);
}

function TokenStatus(props: { token: RuntimeRegistrationToken }) {
	if (props.token.revoked) return <RuntimeStatusPill status="revoked" />;
	if (new Date(props.token.expiresAt).getTime() < Date.now()) return <RuntimeStatusPill status="expired" />;
	return <RuntimeStatusPill status="active" />;
}

function MemberAvatar(props: { member: WorkspaceMember }) {
	const [failed, setFailed] = useState(false);
	const initial = (props.member.name || props.member.email || "M").trim().slice(0, 1).toUpperCase() || "M";
	useEffect(() => {
		setFailed(false);
	}, [props.member.avatarUrl]);

	return (
		<div className="grid size-8 shrink-0 place-items-center overflow-hidden rounded-full bg-[color:var(--selection)] text-[12px] font-semibold text-[color:var(--muted)]">
			{props.member.avatarUrl && !failed ? (
				<img src={props.member.avatarUrl} alt="" className="size-full object-cover" onError={() => setFailed(true)} />
			) : (
				<span>{initial}</span>
			)}
		</div>
	);
}

function RolePill(props: { role: string }) {
	const role = props.role.trim().toLowerCase();
	const tone = role === "owner"
		? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
		: role === "admin"
			? "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]"
			: "bg-[color:var(--block)] text-[color:var(--muted-strong)]";
	return (
		<span className={cn("inline-flex h-6 w-fit items-center rounded-full px-2 text-[12px] font-medium leading-5", tone)}>
			{runtimeStatusLabel(role || "member")}
		</span>
	);
}

function RuntimeStatusPill(props: { status: string }) {
	const normalized = props.status.trim().toLowerCase();
	const tone =
		normalized === "online" || normalized === "active" || normalized === "completed" || normalized === "accepted"
			? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
			: normalized === "running" || normalized === "claimed"
				? "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]"
				: normalized === "queued"
					? "bg-[color:var(--block)] text-[color:var(--muted-strong)]"
					: normalized === "failed" || normalized === "expired" || normalized === "revoked"
						? "bg-[color:var(--danger-soft)] text-[color:var(--danger)]"
						: normalized === "draining"
							? "bg-[color:var(--warning-soft)] text-[color:var(--warning)]"
							: "bg-[color:var(--block)] text-[color:var(--muted)]";
	return (
		<span className={cn("inline-flex h-6 max-w-full items-center gap-1.5 rounded-full px-2 text-[12px] font-medium leading-5", tone)}>
			<Circle data-icon className="size-3" />
			<span className="truncate">{runtimeStatusLabel(normalized)}</span>
		</span>
	);
}

function StatusPill(props: { children: ReactNode }) {
	return (
		<span className="inline-flex h-7 items-center rounded-full bg-[color:var(--block)] px-2.5 text-[12px] font-medium leading-5 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
			{props.children}
		</span>
	);
}

function EmptyRuntimeBlock(props: { icon: LucideIcon; title: string; body: string }) {
	const Icon = props.icon;
	return (
		<div className="grid place-items-center px-6 py-10 text-center">
			<div className="grid size-9 place-items-center rounded-[9px] bg-[color:var(--block)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
				<Icon data-icon />
			</div>
			<div className="mt-3 text-[14px] font-semibold leading-5 text-[color:var(--text)]">{props.title}</div>
			<div className="mt-1 max-w-[46ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">{props.body}</div>
		</div>
	);
}

function LoadingBlock(props: { children: ReactNode }) {
	return <div className="px-4 py-6 text-[13px] leading-6 text-[color:var(--muted)]">{props.children}</div>;
}

function invitationStatus(invitation: WorkspaceInvitation) {
	if (invitation.revoked) return "revoked";
	if (invitation.acceptedAt) return "accepted";
	if (new Date(invitation.expiresAt).getTime() < Date.now()) return "expired";
	return "pending";
}

function activeInvitationCount(invitations: WorkspaceInvitation[]) {
	return invitations.filter((invitation) => invitationStatus(invitation) === "pending").length;
}

function buildInviteLink(token: string) {
	const hash = `#/invite/${encodeURIComponent(token)}`;
	if (typeof window === "undefined") return hash;
	return `${window.location.origin}${window.location.pathname}${hash}`;
}

function buildDockerWorkerCommand(token: string, mode: "personal" | "team") {
	const escapedToken = token.replaceAll("'", "'\\''");
	return `MSPACE_RUNTIME_TOKEN='${escapedToken}' MSPACE_WORKER_MODE='${mode}' scripts/run-server-worker-dev.sh`;
}

function Modal(props: { title: string; description: string; onClose: () => void; children: ReactNode }) {
	const { t } = useMspaceTranslation();

	return (
		<div className="fixed inset-0 z-50 grid place-items-center bg-black/20 px-4 py-6" role="dialog" aria-modal="true" aria-labelledby="workspace-settings-modal-title">
			<div className="w-full max-w-[640px] rounded-[12px] bg-[color:var(--paper)] shadow-[0_24px_80px_rgba(0,0,0,0.18),inset_0_0_0_1px_var(--line)]">
				<div className="flex items-start justify-between gap-4 border-b border-[color:var(--line)] px-5 py-4">
					<div className="min-w-0">
						<h2 id="workspace-settings-modal-title" className="text-[17px] font-semibold leading-6 text-[color:var(--text)]">{props.title}</h2>
						<p className="mt-1 text-[13px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</p>
					</div>
					<Button type="button" variant="ghost" size="icon" aria-label={t("common.close")} onClick={props.onClose}>
						<X data-icon />
					</Button>
				</div>
				<div className="px-5 py-5">{props.children}</div>
			</div>
		</div>
	);
}

function normalizeRuntimeTaskForm(form: RuntimeTaskForm): CreateRuntimeTaskInput {
	const priority = Number(form.priority || 0);
	if (!Number.isFinite(priority) || priority < 0) {
		throw new Error(translate("workspaceSettings.priorityInvalid"));
	}
	const kind = form.kind.trim();
	if (!kind) {
		throw new Error(translate("workspaceSettings.taskKindRequired"));
	}
	return {
		issueId: form.issueId.trim(),
		sessionId: form.sessionId.trim(),
		projectId: form.projectId.trim(),
		kind,
		priority,
		runtimeMode: form.runtimeMode,
		requiredCapabilities: parseJSONObject(form.requiredCapabilities, translate("workspaceSettings.modal.requiredCapabilities")),
		payload: parseJSONObject(form.payload, translate("workspaceSettings.modal.payload")),
	};
}

function parseJSONObject(value: string, label: string): Record<string, unknown> {
	const parsed = JSON.parse(value || "{}") as unknown;
	if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
		throw new Error(translate("workspaceSettings.jsonObjectRequired", { label }));
	}
	return parsed as Record<string, unknown>;
}

function jsonSummary(value: Record<string, unknown>) {
	const keys = Object.keys(value || {});
	if (keys.length === 0) return translate("workspaceSettings.list.none");
	const summary = keys.slice(0, 4).map((key) => `${key}:${String(value[key])}`).join(", ");
	return keys.length > 4 ? `${summary}, ...` : summary;
}

function timeLabel(value: string) {
	if (!value) return "";
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return "";
	return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function runtimeStatusLabel(value: string) {
	const label = translate(`workspaceSettings.status.${value}`, { defaultValue: "" });
	if (label) return label;
	return value.replace(/[_-]+/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}
