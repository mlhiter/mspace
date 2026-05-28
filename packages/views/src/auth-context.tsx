import { createContext, type PropsWithChildren, useContext } from "react";
import type { MspaceUser, MspaceWorkspace } from "@mspace/core";

export interface MspaceAuthContextValue {
  token: string;
  user?: MspaceUser;
  workspaces?: MspaceWorkspace[];
  workspace?: MspaceWorkspace;
  selectedWorkspaceId?: string;
  selectWorkspace?: (workspaceId: string) => void;
  refreshAuth?: () => Promise<unknown>;
  status: "signed-in" | "signed-out" | "loading" | "error";
}

const MspaceAuthContext = createContext<MspaceAuthContextValue>({
  token: "",
  status: "signed-out",
});

export function MspaceAuthProvider(props: PropsWithChildren<{ value: MspaceAuthContextValue }>) {
  return <MspaceAuthContext.Provider value={props.value}>{props.children}</MspaceAuthContext.Provider>;
}

export function useMspaceAuth() {
  return useContext(MspaceAuthContext);
}
