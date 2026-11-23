import { forwardRef, type HTMLAttributes, type ReactNode } from 'react'

export interface ChatPlateProps extends HTMLAttributes<HTMLElement> {
  as?: 'header' | 'main' | 'section' | 'div'
  raised?: boolean
  children: ReactNode
}

/**
 * High-contrast surface plate (88-90% opacity) over CreatureField.
 * Forwarded ref is passed to `<CreatureField avoid={[...]} />` so creatures steer around it.
 */
export const ChatPlate = forwardRef<HTMLElement, ChatPlateProps>(function ChatPlate(
  { as: Tag = 'section', raised = false, className = '', children, ...rest },
  ref,
) {
  const classes = `plate ${raised ? 'plate-raised' : ''} ${className}`.trim()
  return (
    // @ts-expect-error polymorphic intrinsic tag with shared HTMLElement ref
    <Tag ref={ref} className={classes} {...rest}>
      {children}
    </Tag>
  )
})
