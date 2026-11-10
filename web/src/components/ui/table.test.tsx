import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { selectionColumn, Table, type ColumnDef } from './table'

interface Row {
  id: string
  name: string
  score: number
}

const data: Row[] = [
  { id: 'a', name: 'alpha', score: 0.5 },
  { id: 'b', name: 'bravo', score: 0.9 },
  { id: 'c', name: 'charlie', score: 0.1 },
]
const columns: ColumnDef<Row, unknown>[] = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'score', header: 'Score' },
]

const bodyNames = () =>
  screen
    .getAllByRole('row')
    .slice(1)
    .map((r) => within(r).getAllByRole('cell')[0]!.textContent)

describe('Table', () => {
  it('sorts when a header is clicked (numbers descend first)', async () => {
    render(<Table data={data} columns={columns} getRowId={(r) => r.id} />)
    expect(bodyNames()).toEqual(['alpha', 'bravo', 'charlie'])
    const sortBtn = screen.getByRole('button', { name: 'Score' })
    await userEvent.click(sortBtn)
    expect(bodyNames()).toEqual(['bravo', 'alpha', 'charlie'])
    expect(screen.getByRole('columnheader', { name: 'Score' })).toHaveAttribute('aria-sort', 'descending')
    await userEvent.click(sortBtn)
    expect(bodyNames()).toEqual(['charlie', 'alpha', 'bravo'])
  })

  it('supports j/k navigation and Enter to activate', () => {
    const onRowActivate = vi.fn()
    render(<Table data={data} columns={columns} getRowId={(r) => r.id} onRowActivate={onRowActivate} aria-label="Runs" />)
    const region = screen.getByRole('region', { name: 'Runs' })
    fireEvent.keyDown(region, { key: 'j' })
    fireEvent.keyDown(region, { key: 'j' })
    fireEvent.keyDown(region, { key: 'k' })
    fireEvent.keyDown(region, { key: 'j' })
    fireEvent.keyDown(region, { key: 'Enter' })
    expect(onRowActivate).toHaveBeenCalledWith(data[1])
  })

  it('selects rows with the selection column and x', () => {
    const onChange = vi.fn()
    render(
      <Table
        data={data}
        columns={[selectionColumn<Row>(), ...columns]}
        getRowId={(r) => r.id}
        enableRowSelection
        onRowSelectionChange={onChange}
        aria-label="Runs"
      />,
    )
    fireEvent.click(screen.getAllByRole('checkbox', { name: 'Select row' })[2]!)
    expect(onChange).toHaveBeenCalled()
  })

  it('renders the empty state when there are no rows', () => {
    render(<Table data={[]} columns={columns} empty={<p>Nothing here</p>} />)
    expect(screen.getByText('Nothing here')).toBeInTheDocument()
  })
})
