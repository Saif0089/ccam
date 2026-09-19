import { useEffect } from "react";
import { motion, useReducedMotion } from "framer-motion";

// Three claw slashes rake across the brand once on first load, then the overlay
// lifts to reveal the app. It plays exactly once (the caller unmounts it), and
// respects prefers-reduced-motion by resolving immediately with nothing drawn.
export function ClawIntro({ onDone }: { onDone: () => void }) {
  const reduce = useReducedMotion();

  useEffect(() => {
    if (reduce) onDone();
  }, [reduce, onDone]);

  if (reduce) return null;

  return (
    <motion.div
      className="fixed inset-0 z-50 flex items-center justify-center bg-ground"
      initial={{ opacity: 1 }}
      animate={{ opacity: 0 }}
      transition={{ delay: 1.05, duration: 0.45, ease: "easeInOut" }}
      onAnimationComplete={onDone}
      aria-hidden
    >
      <div className="relative">
        <svg viewBox="0 0 420 300" className="w-[68vmin] max-w-[440px]">
          {[0, 1, 2].map((i) => (
            <motion.path
              key={i}
              d={`M ${70 + i * 60} 34 C ${150 + i * 60} 120, ${168 + i * 60} 176, ${196 + i * 60} 262`}
              stroke="#C77DFF"
              strokeWidth={7}
              strokeLinecap="round"
              fill="none"
              initial={{ pathLength: 0, opacity: 0 }}
              animate={{ pathLength: 1, opacity: [0, 1, 1, 0.15] }}
              transition={{ delay: 0.08 + i * 0.11, duration: 0.5, ease: [0.7, 0, 0.3, 1] }}
              style={{ filter: "drop-shadow(0 0 10px rgba(199,125,255,0.55))" }}
            />
          ))}
        </svg>
        <motion.div
          className="absolute inset-0 flex items-center justify-center text-4xl font-semibold tracking-tight text-ink"
          initial={{ opacity: 0, scale: 0.96 }}
          animate={{ opacity: [0, 1, 1], scale: 1 }}
          transition={{ delay: 0.55, duration: 0.5 }}
        >
          clawdh
        </motion.div>
      </div>
    </motion.div>
  );
}
