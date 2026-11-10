import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { Button, IconButton } from './button'
import { ScoreChip } from './score-chip'
import { StatTile } from './stat-tile'
import { StatusPill } from './status-pill'

describe('Button', () => {
  it('fires onClick and defaults to type=button', async () => {
    const onClick = vi.fn()
    render(<Button onClick={onClick}>Run suite</Button>)
    const btn = screen.getByRole('button', { name: 'Run suite' })
    expect(btn).toHaveAttribute('type', 'button')
    await userEvent.click(btn)
    expect(onClick).toHaveBeenCalledOnce()
  })

  it('is disabled and busy while loading', () => {
    render(<Button loading>Saving</Button>)
    const btn = screen.getByRole('button', { name: 'Saving' })
    expect(btn).toBeDisabled()
    expect(btn).toHaveAttribute('aria-busy', 'true')
  })

  it('IconButton exposes its label as the accessible name', () => {
    render(<IconButton label="Copy">x</IconButton>)
    expect(screen.getByRole('button', { name: 'Copy' })).toBeInTheDocument()
  })
})

describe('StatusPill', () => {
  it('always renders a text label, not just color', () => {
    render(<StatusPill status="failed" />)
    expect(screen.getByText('Failed')).toBeInTheDocument()
  })

  it('keeps the label for assistive tech when icon-only', () => {
    render(<StatusPill status="running" iconOnly />)
    expect(screen.getByText('Running')).toHaveClass('sr-only')
  })
})

describe('ScoreChip', () => {
  it('announces pass/fail against the threshold', () => {
    render(
      <>
        <ScoreChip value={0.91} threshold={0.8} />
        <ScoreChip value={0.42} threshold={0.8} />
      </>,
    )
    expect(screen.getByLabelText('Score 0.91, passed')).toBeInTheDocument()
    expect(screen.getByLabelText('Score 0.42, failed')).toBeInTheDocument()
  })

  it('renders a dash for missing scores', () => {
    render(<ScoreChip value={null} />)
    expect(screen.getByLabelText('Score —')).toBeInTheDocument()
  })
})

describe('StatTile', () => {
  it('colors deltas by whether up is good', () => {
    render(<StatTile label="p95 latency" value="1.4 s" delta={{ value: '+180 ms', direction: 'up', upIsGood: false }} />)
    expect(screen.getByText('+180 ms')).toHaveClass('text-fail')
  })
})
