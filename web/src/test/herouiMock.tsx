import React, { createContext, useContext } from 'react';

type ChildrenProps = {
  children?: React.ReactNode;
  className?: string;
};

type ButtonProps = ChildrenProps & {
  'aria-busy'?: boolean;
  'aria-label'?: string;
  form?: string;
  isDisabled?: boolean;
  onPress?: () => void;
  size?: string;
  type?: 'button' | 'submit' | 'reset';
  variant?: string;
};

export function Button({
  children,
  isDisabled,
  onPress,
  type = 'button',
  ...props
}: ButtonProps) {
  return (
    <button
      aria-busy={props['aria-busy']}
      aria-label={props['aria-label']}
      disabled={isDisabled}
      form={props.form}
      type={type}
      onClick={() => onPress?.()}
    >
      {children}
    </button>
  );
}

export function Form({ children, className, ...props }: ChildrenProps & React.FormHTMLAttributes<HTMLFormElement>) {
  return <form className={className} {...props}>{children}</form>;
}

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} />;
}

export function TextArea(props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} />;
}

export function Label({ children }: ChildrenProps) {
  return <label>{children}</label>;
}

export function FieldError({ children }: ChildrenProps) {
  return <div role="alert">{children}</div>;
}

export function Description({ children }: ChildrenProps) {
  return <p>{children}</p>;
}

export function Spinner() {
  return <span data-testid="spinner" />;
}

export function TextField({ children }: ChildrenProps) {
  return <div>{children}</div>;
}

export const Alert = Object.assign(
  function AlertRoot({ children, status }: ChildrenProps & { status?: string }) {
    return <div data-status={status} role="alert">{children}</div>;
  },
  {
    Content: function AlertContent({ children }: ChildrenProps) {
      return <div>{children}</div>;
    },
    Description: function AlertDescription({ children }: ChildrenProps) {
      return <span>{children}</span>;
    },
    Indicator: function AlertIndicator({ children }: ChildrenProps) {
      return <span>{children}</span>;
    },
  },
);

export const Card = Object.assign(
  function CardRoot({ children }: ChildrenProps) {
    return <section>{children}</section>;
  },
  {
    Content: function CardContent({ children }: ChildrenProps) {
      return <div>{children}</div>;
    },
  },
);

type TabsContextValue = {
  onSelectionChange?: (key: string) => void;
  selectedKey?: string;
};

const TabsContext = createContext<TabsContextValue>({});

export const Tabs = Object.assign(
  function TabsRoot({
    children,
    onSelectionChange,
    selectedKey,
  }: ChildrenProps & TabsContextValue) {
    return (
      <TabsContext.Provider value={{ onSelectionChange, selectedKey }}>
        <div>{children}</div>
      </TabsContext.Provider>
    );
  },
  {
    List: function TabsList({ children }: ChildrenProps) {
      return <div role="tablist">{children}</div>;
    },
    Tab: function TabsTab({ children, id }: ChildrenProps & { id: string }) {
      const tabs = useContext(TabsContext);
      return (
        <button
          aria-selected={tabs.selectedKey === id}
          role="tab"
          type="button"
          onClick={() => tabs.onSelectionChange?.(id)}
        >
          {children}
        </button>
      );
    },
    Indicator: function TabsIndicator() {
      return null;
    },
    Separator: function TabsSeparator() {
      return null;
    },
  },
);

export const Link = ({ children, href }: ChildrenProps & { href?: string }) => (
  <a href={href}>{children}</a>
);

export function useOverlayState({
  isOpen,
  onOpenChange,
}: {
  isOpen: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  return {
    close: () => onOpenChange?.(false),
    isOpen,
    open: () => onOpenChange?.(true),
    setOpen: onOpenChange,
  };
}

export const Modal = Object.assign(
  function ModalRoot({ children, state }: ChildrenProps & { state?: { isOpen?: boolean } }) {
    return state?.isOpen === false ? null : <div>{children}</div>;
  },
  {
    Backdrop: function ModalBackdrop({ children }: ChildrenProps) {
      return <div>{children}</div>;
    },
    Body: function ModalBody({ children }: ChildrenProps) {
      return <div>{children}</div>;
    },
    CloseTrigger: function ModalCloseTrigger() {
      return <button aria-label="close" type="button" />;
    },
    Container: function ModalContainer({ children }: ChildrenProps) {
      return <div>{children}</div>;
    },
    Dialog: function ModalDialog({ children }: ChildrenProps) {
      return <div>{children}</div>;
    },
    Footer: function ModalFooter({ children }: ChildrenProps) {
      return <footer>{children}</footer>;
    },
    Header: function ModalHeader({ children }: ChildrenProps) {
      return <header>{children}</header>;
    },
    Heading: function ModalHeading({ children }: ChildrenProps) {
      return <h2>{children}</h2>;
    },
  },
);

export const Checkbox = Object.assign(
  function CheckboxRoot({
    children,
    isSelected,
    onChange,
  }: ChildrenProps & { isSelected?: boolean; onChange?: () => void }) {
    return (
      <label>
        <input checked={!!isSelected} type="checkbox" onChange={() => onChange?.()} />
        {children}
      </label>
    );
  },
  {
    Content: function CheckboxContent({ children }: ChildrenProps) {
      return <span>{children}</span>;
    },
    Control: function CheckboxControl({ children }: ChildrenProps) {
      return <span>{children}</span>;
    },
    Indicator: function CheckboxIndicator() {
      return <span />;
    },
  },
);
