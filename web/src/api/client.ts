export type Session = {
  id: string;
  name: string;
  target: string;
  status: string;
  started_at: string;
  ended_at?: string;
};

export async function fetchCurrentSession(): Promise<Session> {
  const response = await fetch("/api/v1/session/current");
  if (!response.ok) {
    throw new Error("failed to fetch current session");
  }

  return response.json();
}
