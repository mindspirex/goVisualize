import type { Metadata } from 'next';
import '@xyflow/react/dist/style.css';
import '@/app/globals.css';

export const metadata: Metadata = { title: 'govisualize — Function tree', description: 'Explore the calls behind your Go project.' };
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return <html lang="en"><body>{children}</body></html>;
}
