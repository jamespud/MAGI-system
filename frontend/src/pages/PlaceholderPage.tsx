import { Card } from '@/components/ui';

interface PlaceholderPageProps {
  title: string;
}

export default function PlaceholderPage({ title }: PlaceholderPageProps) {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <Card className="text-center max-w-md">
        <h1 className="font-mono text-xl font-bold text-text-primary mb-2">{title}</h1>
        <p className="text-sm text-text-muted font-mono">Coming soon</p>
      </Card>
    </div>
  );
}
