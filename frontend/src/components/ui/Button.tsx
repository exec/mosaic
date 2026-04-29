import {splitProps, type ComponentProps} from 'solid-js';

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger';

const styles: Record<Variant, string> = {
  primary:   'bg-accent-500 text-white shadow-sm hover:bg-accent-400 focus-visible:ring-accent-400',
  secondary: 'border border-white/10 bg-white/[.02] text-zinc-100 hover:bg-white/[.04] focus-visible:ring-white/30',
  ghost:     'text-zinc-300 hover:bg-white/[.04] hover:text-zinc-100 focus-visible:ring-white/30',
  danger:    'bg-rose-600 text-white hover:bg-rose-500 focus-visible:ring-rose-400',
};

export function Button(props: ComponentProps<'button'> & {variant?: Variant}) {
  const [local, rest] = splitProps(props, ['variant', 'class', 'children']);
  const variant = local.variant ?? 'secondary';
  return (
    <button
      {...rest}
      class={`inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-950 disabled:opacity-50 disabled:pointer-events-none ${styles[variant]} ${local.class ?? ''}`}
    >
      {local.children}
    </button>
  );
}
