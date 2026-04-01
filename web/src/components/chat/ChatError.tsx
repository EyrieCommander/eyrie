// ChatError.tsx — Shared error banner for chat components.

export function ChatError({ message }: { message: string }) {
  return (
    <div className="rounded border border-red/30 bg-red/5 px-4 py-2 text-[10px] text-red">
      {message}
    </div>
  );
}
