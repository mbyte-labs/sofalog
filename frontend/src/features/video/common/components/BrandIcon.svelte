<script>
  // One 24×24 brand mark from lib/brandMarks.js, sized and filled by the
  // caller. Decorative by default: pass a label only when the mark is the
  // only thing naming what it stands for.

  /**
   * @type {{
   *   path: string,
   *   size?: number,
   *   fill?: string,
   *   label?: string | null,
   * }}
   */
  let { path, size = 24, fill = 'currentColor', label = null } = $props()
</script>

<svg
  viewBox="0 0 24 24"
  width={size}
  height={size}
  role={label ? 'img' : null}
  aria-label={label}
  aria-hidden={label ? null : 'true'}
>
  <path d={path} {fill} />
</svg>
