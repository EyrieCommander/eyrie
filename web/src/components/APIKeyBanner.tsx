import { AlertCircle } from "lucide-react";

interface APIKeyBannerProps {
  hint: string;
}

export default function APIKeyBanner({ hint }: APIKeyBannerProps) {
  if (!hint) return null;

  return (
    <div className="mb-6 p-4 bg-yellow-500/10 border border-yellow-500/20 rounded-lg">
      <div className="flex items-start gap-3">
        <AlertCircle className="w-5 h-5 text-yellow-500 flex-shrink-0 mt-0.5" />
        <div>
          <h3 className="text-sm font-medium text-fg mb-1">api key required</h3>
          <p className="text-sm text-fg-muted whitespace-pre-line">{hint}</p>
        </div>
      </div>
    </div>
  );
}
