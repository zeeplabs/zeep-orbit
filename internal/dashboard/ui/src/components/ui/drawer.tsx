import * as React from "react"
import * as DialogPrimitive from "@radix-ui/react-dialog"

import { cn } from "@/lib/utils"
import { Icon } from "@/components/ui/icon"

/**
 * Drawer lateral (right-side), sobre Radix Dialog. Base do chat "Create with AI"
 * e de formulários de config (ver spec dashboard-redesign, FormDrawer nível 2).
 */
const Drawer = DialogPrimitive.Root
const DrawerTrigger = DialogPrimitive.Trigger
const DrawerClose = DialogPrimitive.Close
const DrawerPortal = DialogPrimitive.Portal

const DrawerOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      "fixed inset-0 z-50 bg-[var(--overlay)] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
      className
    )}
    {...props}
  />
))
DrawerOverlay.displayName = "DrawerOverlay"

const DrawerContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & {
    /** Largura do drawer (default 440px). */
    width?: number
    hideClose?: boolean
  }
>(({ className, children, width = 440, hideClose, ...props }, ref) => (
  <DrawerPortal>
    <DrawerOverlay />
    <DialogPrimitive.Content
      ref={ref}
      style={{ width, maxWidth: "100vw" }}
      className={cn(
        "fixed right-0 top-0 z-50 flex h-full flex-col border-l border-[var(--border)] bg-[var(--surface)] shadow-[var(--shadow-md)] duration-300 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right",
        className
      )}
      {...props}
    >
      {children}
      {!hideClose && (
        <DialogPrimitive.Close className="absolute right-4 top-4 rounded-lg p-1 opacity-70 transition-opacity hover:opacity-100 hover:bg-[var(--hover-surface)] focus:outline-none focus:ring-2 focus:ring-ring">
          <Icon name="close" size={18} />
          <span className="sr-only">Close</span>
        </DialogPrimitive.Close>
      )}
    </DialogPrimitive.Content>
  </DrawerPortal>
))
DrawerContent.displayName = "DrawerContent"

const DrawerHeader = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("flex flex-col gap-1 border-b border-[var(--border)] p-5", className)} {...props} />
)
DrawerHeader.displayName = "DrawerHeader"

const DrawerBody = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("flex-1 overflow-y-auto p-5", className)} {...props} />
)
DrawerBody.displayName = "DrawerBody"

const DrawerFooter = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("flex items-center justify-end gap-2 border-t border-[var(--border)] p-5", className)} {...props} />
)
DrawerFooter.displayName = "DrawerFooter"

const DrawerTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title
    ref={ref}
    className={cn("text-lg font-semibold leading-none tracking-tight", className)}
    style={{ fontFamily: "var(--font-display)" }}
    {...props}
  />
))
DrawerTitle.displayName = "DrawerTitle"

const DrawerDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn("text-sm text-[var(--text-secondary)]", className)}
    {...props}
  />
))
DrawerDescription.displayName = "DrawerDescription"

export {
  Drawer,
  DrawerTrigger,
  DrawerClose,
  DrawerPortal,
  DrawerOverlay,
  DrawerContent,
  DrawerHeader,
  DrawerBody,
  DrawerFooter,
  DrawerTitle,
  DrawerDescription,
}
