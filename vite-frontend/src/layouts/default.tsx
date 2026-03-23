import { Navbar } from "@/components/navbar";

export default function DefaultLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="relative flex flex-col min-h-screen bg-transparent">
      <Navbar />
      <main className="container mx-auto max-w-7xl px-4 sm:px-6 flex-grow pt-4 sm:pt-8 pb-6">
        <div className="panel-shell p-4 sm:p-6">
          {children}
        </div>
      </main>
    </div>
  );
}
