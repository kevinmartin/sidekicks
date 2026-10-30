/** Wire boundaries only; this file does not create model-backed agents. */
export type CapabilityStatus = 'tested' | 'implemented' | 'unsupported' | 'untested';
export type AdapterId = 'novnc' | 'neko';
export type Owner = 'human' | 'agent';
export type InputEvent =
  | { type: 'key'; key: string; action: 'down' | 'up' }
  | { type: 'text'; text: string }
  | { type: 'pointer'; x: number; y: number; action: 'move' | 'down' | 'up'; button?: 'left' | 'middle' | 'right' }
  | { type: 'click'; x: number; y: number; button: 'left' | 'right'; count: 1 | 2 }
  | { type: 'scroll'; x: number; y: number; deltaX: number; deltaY: number }
  | { type: 'releaseAll' };
export interface ControlLease { token: string; session: string; owner: Owner; expiresAt: string }
export interface ComputerAdapter {
  health(): Promise<{ ok: boolean; status: string }>;
  capabilities(): Promise<Record<string, { status: CapabilityStatus; detail?: string }>>;
  acquire(owner: Owner, session: string, leaseMs: number): Promise<ControlLease>;
  release(lease: ControlLease): Promise<void>;
  stop(): Promise<void>;
  input(lease: ControlLease, events: InputEvent[]): Promise<void>;
  screenshot(bounds: { maxWidth: number; maxHeight: number }): Promise<Blob>;
}
/** Planned terminal implementations must accept a disposable sandbox ID, never a host shell command. */
export interface TerminalAdapter { execute(sandboxId: string, argv: string[], timeoutMs: number): Promise<{ exitCode: number; stdout: string; stderr: string }> }
/** Browser implementations create task-owned contexts and close only those contexts. */
export interface BrowserAdapter { newContext(): Promise<{ id: string }>; navigate(contextId: string, url: string): Promise<void>; closeContext(contextId: string): Promise<void> }
export interface ThreadIntent { bot: 'Scout' | 'Builder' | 'Reviewer'; intent: string; at: string; execution: 'intent-only' }
