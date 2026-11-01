import {
  Github,
  Webhook,
  Clock,
  Server,
  SquareTerminal,
  Activity,
  CircleCheckBig,
  GitBranch,
  Database,
  Flag,
  Globe,
  Hourglass,
  MapPin,
  Scaling,
  Image as ImageIcon,
  KeyRound,
  FileText,
  Users,
  Bot,
  CircleHelp,
  type LucideIcon,
} from "lucide-react";

// Map our icon keys (stored on nodes/fields) to lucide components.
const MAP: Record<string, LucideIcon> = {
  github: Github,
  webhook: Webhook,
  clock: Clock,
  server: Server,
  terminal: SquareTerminal,
  heart: Activity,
  approve: CircleCheckBig,
  branch: GitBranch,
  db: Database,
  flag: Flag,
  globe: Globe,
  wait: Hourglass,
  pin: MapPin,
  size: Scaling,
  img: ImageIcon,
  key: KeyRound,
  file: FileText,
  user: Users,
  agent: Bot,
};

export function Icon({ name, size = 16 }: { name?: string; size?: number }) {
  if (!name) return null;
  const C = MAP[name] ?? CircleHelp;
  return <C size={size} strokeWidth={2} />;
}
